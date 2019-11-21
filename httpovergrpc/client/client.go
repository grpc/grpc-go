/*
 *
 * Copyright 2019 gRPC authors.
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

package httpovergrpc

import (
	"bufio"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	pb "google.golang.org/grpc/httpovergrpc/http_over_grpc_proto"
)

// Transport is an http.Transport that communicates via a HTTPOverGRPC.
type Transport struct {
	ctx  context.Context
	conn *grpc.ClientConn
	// HTTPOverGRPC client.
	client pb.HTTPOverGRPCClient
}

// Close closes the HTTPOverGRPC connection.
func (t *Transport) Close() error {
	if t.conn != nil {
		grpclog.Infof("Closing httpovergrpc connection.")
		return t.conn.Close()
	}
	return nil
}

type wrappedBody struct {
	reader io.Reader
	pr     *io.PipeReader
}

func (h *wrappedBody) Close() error {
	return h.pr.Close()
}

func (h *wrappedBody) Read(p []byte) (n int, err error) {
	return h.reader.Read(p)
}

// NewTransport returns a new Transport for making HTTP over gRPC calls.
func NewTransport(ctx context.Context, conn *grpc.ClientConn) *Transport {
	return &Transport{
		ctx:    ctx,
		conn:   conn,
		client: pb.NewHTTPOverGRPCClient(conn),
	}
}

// RoundTrip implements the http.RoundTripper interface.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := t.ctx
	if ctx == context.Background() || ctx == nil {
		ctx = req.Context()
	}
	grpclog.Infof("%s %q", req.Method, req.URL.String())

	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		if body, err = ioutil.ReadAll(req.Body); err != nil {
			grpclog.Errorf("Error reading body: %v", err)
			return nil, err
		}
	}

	hReq := &pb.HTTPOverGRPCRequest{
		Method:  req.Method,
		Url:     req.URL.String(),
		Headers: fromHeader(req.Header),
		Body:    body,
	}

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(t.doRequest(ctx, pw, hReq))
	}()

	type responseOrError struct {
		resp *http.Response
		err  error
	}
	// This needs to be buffered, otherwise the goroutine will leak.
	respCh := make(chan responseOrError, 1)
	go func() {
		resp, err := readResponse(pr, req)
		respCh <- responseOrError{resp, err}
	}()
	timeout := func(usage string) (*http.Response, error) {
		pr.Close()
		grpclog.Infof("Response header timeout: %v", usage)
		return nil, errors.New("timeout before response header received")
	}
	select {
	case re := <-respCh:
		return re.resp, re.err
	case <-ctx.Done():
		return timeout("ctx.Done")
	}
}

// doRequest makes the HTTPOverGRPC HTTPRequest and handles the response.
func (t *Transport) doRequest(ctx context.Context, w io.Writer, req *pb.HTTPOverGRPCRequest) error {
	// gRPC deadline becomes the HTTP request timeout.
	ctx, cancel := context.WithCancel(ctx)
	deadline, ok := ctx.Deadline()
	if ok {
		grpclog.Infof("Using Transport.Timeout = %v (plus epsilon)", deadline)
		tt := deadline.Sub(time.Now())
		ctx, cancel = context.WithTimeout(ctx, tt+100*time.Millisecond)
	}
	defer cancel()

	resp, err := t.client.HTTPRequest(ctx, req)
	if err != nil {
		return err
	}

	if _, err = w.Write(resp.Body); err != nil {
		grpclog.Errorf("error writing response: %v", err)
		return err
	}

	return nil
}

// readResponse reads the HTTP response from the pipe reader and returns the
// http response.
func readResponse(pr *io.PipeReader, req *http.Request) (resp *http.Response, err error) {
	defer func() {
		if err != nil {
			pr.CloseWithError(err)
		}
	}()
	tp := textproto.NewReader(bufio.NewReader(pr))
	res, err := http.ReadResponse(bufio.NewReader(tp.R), req)
	if err != nil {
		return nil, err
	}
	// Wrap the body and pipe reader to ensure both are closed.
	res.Body = &wrappedBody{
		reader: res.Body,
		pr:     pr,
	}
	return res, nil
}

// fromHeader converts an http.Header to the repeated pb.Header proto field.
func fromHeader(hdrs http.Header) []*pb.Header {
	result := make([]*pb.Header, 0, len(hdrs))
	for k, vals := range hdrs {
		result = append(result, &pb.Header{Key: k, Values: vals})
	}
	return result
}
