/*
 *
 * Copyright 2026 gRPC authors.
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

package xdsclient

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/protobuf/proto"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// FakeTransportBuilder implements clients.TransportBuilder.
type FakeTransportBuilder struct {
	// ActiveTransports tracks created transports for the fuzzer to interact with.
	// Map key can be the server URI.
	ActiveTransports map[string]*FakeTransport
	mu               sync.Mutex
}

func NewFakeTransportBuilder() *FakeTransportBuilder {
	return &FakeTransportBuilder{
		ActiveTransports: make(map[string]*FakeTransport),
	}
}

func (b *FakeTransportBuilder) Build(serverIdentifier clients.ServerIdentifier) (clients.Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ft := NewFakeTransport(serverIdentifier.ServerURI)
	b.ActiveTransports[serverIdentifier.ServerURI] = ft
	return ft, nil
}

// GetTransport returns the active transport for a given server URI.
func (b *FakeTransportBuilder) GetTransport(serverURI string) *FakeTransport {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ActiveTransports[serverURI]
}

// FakeTransport implements clients.Transport.
type FakeTransport struct {
	ServerURI string
	// ActiveStreams tracks active streams for the fuzzer.
	// We assume one ADS stream for simplicity in basic fuzzing, or map by method.
	ActiveAdsStream *FakeStream
	mu              sync.Mutex
	closed          bool
}

func NewFakeTransport(serverURI string) *FakeTransport {
	return &FakeTransport{
		ServerURI: serverURI,
	}
}

func (t *FakeTransport) NewStream(ctx context.Context, method string) (clients.Stream, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, fmt.Errorf("transport closed")
	}

	// We only care about ADS for now in this fuzzer
	fs := NewFakeStream(ctx)
	t.ActiveAdsStream = fs
	return fs, nil
}

func (t *FakeTransport) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.ActiveAdsStream != nil {
		t.ActiveAdsStream.Close()
	}
}

// FakeStream implements clients.Stream.
type FakeStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex

	// reqQueue stores marshaled requests sent by the client
	reqQueue [][]byte

	// respQueue stores responses to be returned to the client
	respQueue [][]byte

	// signals
	reqSignal  chan struct{}
	respSignal chan struct{}
}

func NewFakeStream(ctx context.Context) *FakeStream {
	c, cancel := context.WithCancel(ctx)
	return &FakeStream{
		ctx:        c,
		cancel:     cancel,
		reqSignal:  make(chan struct{}, 1),
		respSignal: make(chan struct{}, 1),
	}
}

func (s *FakeStream) Send(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return io.EOF
	}
	s.reqQueue = append(s.reqQueue, data)
	select {
	case s.reqSignal <- struct{}{}:
	default:
	}
	return nil
}

func (s *FakeStream) Recv() ([]byte, error) {
	for {
		s.mu.Lock()
		if len(s.respQueue) > 0 {
			data := s.respQueue[0]
			s.respQueue = s.respQueue[1:]
			s.mu.Unlock()
			return data, nil
		}
		if s.ctx.Err() != nil {
			s.mu.Unlock()
			return nil, io.EOF
		}
		s.mu.Unlock()

		select {
		case <-s.respSignal:
		case <-s.ctx.Done():
			return nil, io.EOF
		}
	}
}

func (s *FakeStream) Close() {
	s.cancel()
}

// Fuzzer Helper Methods

// ReadRequest returns the next request sent by the client.
func (s *FakeStream) ReadRequest(timeout time.Duration) (*v3discoverypb.DiscoveryRequest, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		s.mu.Lock()
		if len(s.reqQueue) > 0 {
			data := s.reqQueue[0]
			s.reqQueue = s.reqQueue[1:]
			s.mu.Unlock()

			req := &v3discoverypb.DiscoveryRequest{}
			if err := proto.Unmarshal(data, req); err != nil {
				return nil, fmt.Errorf("failed to unmarshal request: %v", err)
			}
			return req, nil
		}
		s.mu.Unlock()

		select {
		case <-s.reqSignal:
		case <-timer.C:
			return nil, fmt.Errorf("timeout waiting for request")
		}
	}
}

// InjectResponse queues a response for the client to receive.
func (s *FakeStream) InjectResponse(resp *v3discoverypb.DiscoveryResponse) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.respQueue = append(s.respQueue, data)
	s.mu.Unlock()

	select {
	case s.respSignal <- struct{}{}:
	default:
	}
	return nil
}

// InjectRawResponse queues a raw byte response for the client.
func (s *FakeStream) InjectRawResponse(data []byte) {
	s.mu.Lock()
	s.respQueue = append(s.respQueue, data)
	s.mu.Unlock()

	select {
	case s.respSignal <- struct{}{}:
	default:
	}
}
