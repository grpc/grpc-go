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

// Package faketransport provides a fake implementation of the
// clients.TransportBuilder interface for testing purposes.
package faketransport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/protobuf/proto"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Builder implements clients.TransportBuilder.
type Builder struct {
	mu               sync.Mutex
	ActiveTransports map[string]*FakeTransport // Tracks created transports for the fuzzer to interact with.
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		ActiveTransports: make(map[string]*FakeTransport),
	}
}

// Build creates a new Transport for the given server identifier.
func (b *Builder) Build(serverIdentifier clients.ServerIdentifier) (clients.Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.ActiveTransports[serverIdentifier.ServerURI]; ok {
		return b.ActiveTransports[serverIdentifier.ServerURI], nil
	}

	ft := NewFakeTransport()
	b.ActiveTransports[serverIdentifier.ServerURI] = ft
	return ft, nil
}

// GetTransport returns the active transport for a given server URI.
func (b *Builder) GetTransport(serverURI string) *FakeTransport {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			b.mu.Lock()
			if t, ok := b.ActiveTransports[serverURI]; ok && t.ActiveAdsStream != nil {
				b.mu.Unlock()
				return t
			}
			b.mu.Unlock()
		}
	}
}

// FakeTransport implements clients.Transport.
type FakeTransport struct {
	mu              sync.Mutex
	ActiveAdsStream *FakeStream
	closed          bool
}

// NewFakeTransport creates a FakeTransport for the given server URI.
func NewFakeTransport() *FakeTransport {
	return &FakeTransport{}
}

// NewStream creates a new fake stream to the server.
func (t *FakeTransport) NewStream(ctx context.Context, _ string) (clients.Stream, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil, fmt.Errorf("transport is closed")
	}

	t.ActiveAdsStream = newFakeStream(ctx)
	return t.ActiveAdsStream, nil
}

// Close closes the fake stream.
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

	mu        sync.Mutex
	reqQueue  [][]byte // reqQueue stores marshaled requests sent by the client
	respQueue [][]byte // respQueue stores responses to be returned to the client

	reqChan  chan struct{} // reqChan signals that the request queue is non-empty.
	respChan chan struct{} // respChan signals that the response queue is non-empty.
}

func newFakeStream(ctx context.Context) *FakeStream {
	c, cancel := context.WithCancel(ctx)
	return &FakeStream{
		ctx:      c,
		cancel:   cancel,
		reqChan:  make(chan struct{}, 1),
		respChan: make(chan struct{}, 1),
	}
}

// Send sends the provided message on the stream. It appends the request to the
// reqQueue and signals the reqChan to notify that a request is available.
func (s *FakeStream) Send(data []byte) error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.reqQueue = append(s.reqQueue, data)
	select {
	case s.reqChan <- struct{}{}:
	default:
	}
	return nil
}

// Recv blocks until the next message is received on the stream. It blocks until
// a response is available in the respQueue (pushed via InjectResponse) or the
// context is canceled.
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
			return nil, s.ctx.Err()
		}
		s.mu.Unlock()

		select {
		case <-s.respChan:
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
}

// Close closes the fake stream.
func (s *FakeStream) Close() {
	s.cancel()
}

// ReadRequest reads the next request from the reqQueue. It blocks until a
// request is available or the timeout expires. It returns an error if the
// timeout expires or if the request cannot be unmarshaled.
func (s *FakeStream) ReadRequest() (*v3discoverypb.DiscoveryRequest, error) {
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
		case <-s.reqChan:
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
}

// InjectResponse simulates a server response. It marshals the provided
// DiscoveryResponse, queues it in the respQueue, and signals the respChan to
// notify that a response is available for the client to Recv.
func (s *FakeStream) InjectResponse(resp *v3discoverypb.DiscoveryResponse) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.respQueue = append(s.respQueue, data)
	s.mu.Unlock()

	select {
	case s.respChan <- struct{}{}:
	default:
	}
	return nil
}
