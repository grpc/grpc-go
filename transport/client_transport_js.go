// +build js,wasm

/*
 *
 * Copyright 2018 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package transport

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"golang.org/x/net/context"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// newHTTP2Client constructs a connected ClientTransport to addr based on HTTP2
// and starts to receive messages on it. Non-nil error returns if construction
// fails.
func newHTTP2Client(connectCtx, ctx context.Context, addr TargetInfo, opts ConnectOptions, onSuccess func()) (_ ClientTransport, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		if err != nil {
			cancel()
		}
	}()

	t := &http2Client{
		ctx:                   ctx,
		cancel:                cancel,
		md:                    addr.Metadata,
		readerDone:            make(chan struct{}),
		writerDone:            make(chan struct{}),
		goAway:                make(chan struct{}),
		activeStreams:         make(map[uint32]*Stream),
		creds:                 opts.PerRPCCredentials,
		statsHandler:          opts.StatsHandler,
		initialWindowSize:     initialWindowSize,
		nextID:                1,
		maxConcurrentStreams:  defaultMaxStreamsClient,
		streamQuota:           defaultMaxStreamsClient,
		streamsQuotaAvailable: make(chan struct{}, 1),
	}
	if opts.StatsHandler != nil {
		t.ctx = opts.StatsHandler.TagConn(t.ctx, &stats.ConnTagInfo{
			RemoteAddr: t.remoteAddr,
			LocalAddr:  t.localAddr,
		})
		connBegin := &stats.ConnBegin{
			Client: true,
		}
		t.statsHandler.HandleConn(t.ctx, connBegin)
	}
	defer onSuccess()
	return t, nil
}

func (t *http2Client) NewStream(ctx context.Context, callHdr *CallHdr) (*Stream, error) {
	ctx = peer.NewContext(ctx, t.getPeer())
	ctx, cancel := context.WithCancel(ctx)
	s := &Stream{
		ctx:        ctx,
		cancel:     cancel,
		headerChan: make(chan struct{}),
		respChan:   make(chan struct{}),
		done:       make(chan struct{}),
	}

	endpoint := callHdr.Method
	if callHdr.Host != "" {
		endpoint = callHdr.Host + "/" + endpoint
	}

	req, err := http.NewRequest(
		"POST",
		endpoint,
		nil,
	)
	if err != nil {
		return nil, err
	}

	s.req = req.WithContext(s.ctx)

	headerFields, err := t.createHeaderFields(s.ctx, callHdr)
	if err != nil {
		return nil, err
	}
	for _, hf := range headerFields {
		// Filter out HTTP2 metadata headers
		if strings.HasPrefix(hf.Name, ":") {
			continue
		}
		// Don't add the user-agent header
		if strings.ToLower(hf.Name) == "user-agent" {
			continue
		}

		req.Header.Add(hf.Name, hf.Value)
	}

	// Set gRPC-Web specific headers
	// https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-WEB.md#protocol-differences-vs-grpc-over-http2
	req.Header.Add("X-Grpc-Web", "1")
	req.Header.Add("X-User-Agent", "grpc-web-javascript/0.1")

	if t.statsHandler != nil {
		outHeader := &stats.OutHeader{
			Client:      true,
			FullMethod:  callHdr.Method,
			RemoteAddr:  t.remoteAddr,
			LocalAddr:   t.localAddr,
			Compression: callHdr.SendCompress,
		}
		t.statsHandler.HandleRPC(s.ctx, outHeader)
	}

	return s, nil
}

func (t *http2Client) Write(s *Stream, hdr []byte, data []byte, opts *Options) error {
	if atomic.SwapUint32(&s.requestSent, 1) == 1 {
		return status.Error(codes.Unimplemented, "multiple Write's are not supported.")
	}

	s.req.Body = ioutil.NopCloser(bytes.NewBuffer(append(hdr, data...)))

	resp, err := http.DefaultClient.Do(s.req)
	if err != nil {
		return err
	}

	err = t.operateHeaders(s, resp.Header, &s.req.URL.Path, &resp.StatusCode)
	if err != nil {
		resp.Body.Close()
		s.writeErr = err
		close(s.respChan)
		return err
	}

	s.body = resp.Body
	close(s.respChan)
	return nil
}

func (t *http2Client) Close() error {
	if t.statsHandler != nil {
		connEnd := &stats.ConnEnd{
			Client: true,
		}
		t.statsHandler.HandleConn(t.ctx, connEnd)
	}
	return nil
}
func (t *http2Client) GracefulClose() error {
	return nil
}
func (t *http2Client) GetGoAwayReason() GoAwayReason {
	return 0
}
func (t *http2Client) CloseStream(stream *Stream, err error) {
	if stream.cancel != nil {
		stream.cancel()
	}
	select {
	case <-stream.respChan:
		if stream.body != nil {
			stream.body.Close()
		}
	default:
	}
}
func (t *http2Client) IncrMsgSent() {}
func (t *http2Client) IncrMsgRecv() {}

func (d *decodeState) decodeResponseHeader(headers http.Header, path *string, status *int) error {
	for key, values := range headers {
		for _, value := range values {
			if err := d.processHeaderField(hpack.HeaderField{Name: strings.ToLower(key), Value: value}); err != nil {
				return err
			}
		}
	}
	if path != nil {
		if err := d.processHeaderField(hpack.HeaderField{Name: ":path", Value: *path}); err != nil {
			return err
		}
	}
	if status != nil {
		if err := d.processHeaderField(hpack.HeaderField{Name: ":status", Value: strconv.Itoa(*status)}); err != nil {
			return err
		}
	}
	return d.validate()
}

// operateHeaders takes action on the decoded headers.
func (t *http2Client) operateHeaders(s *Stream, headers http.Header, path *string, status *int) error {
	atomic.StoreUint32(&s.bytesReceived, 1)
	var state decodeState
	if err := state.decodeResponseHeader(headers, path, status); err != nil {
		return err
	}

	defer func() {
		if t != nil && t.statsHandler != nil {
			inHeader := &stats.InHeader{
				Client: true,
			}
			t.statsHandler.HandleRPC(s.ctx, inHeader)
		}
	}()
	// If headers haven't been received yet.
	if atomic.SwapUint32(&s.headerDone, 1) == 0 {
		// These values can be set without any synchronization because
		// stream goroutine will read it only after seeing a closed
		// headerChan which we'll close after setting this.
		s.recvCompress = state.encoding
		if len(state.mdata) > 0 {
			s.header = state.mdata
		}
		close(s.headerChan)
	}
	if state.rawStatusCode == nil || state.rawStatusMsg == "" || state.statusGen == nil {
		return nil
	}

	if s.swapState(streamDone) == streamDone {
		// If it was already done, return.
		return nil
	}
	s.status = state.status()
	if len(state.mdata) > 0 {
		s.trailer = state.mdata
	}
	close(s.done)
	return nil
}

// SetTrailers sets the trailers on the stream. This is unique to the WASM client
// as trailers are not read by the stream but by the user of the stream.
func (s *Stream) SetTrailers(trailers http.Header) error {
	return (*http2Client)(nil).operateHeaders(s, trailers, nil, nil)
}
