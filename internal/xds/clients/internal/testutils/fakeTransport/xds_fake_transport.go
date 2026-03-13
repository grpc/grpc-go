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

// Package faketransport provides a fake implementation of the xDS client's
// transport layer. It implements the clients.TransportBuilder,
// clients.Transport and clients.Stream interfaces for testing purposes.
package faketransport

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/protobuf/proto"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// This compile time checks ensures that the Builder, transport and stream
// implementations satisfy the required interfaces.
var _ clients.TransportBuilder = &Builder{}
var _ clients.Transport = &transport{}
var _ clients.Stream = &stream{}

// Builder implements clients.TransportBuilder.
type Builder struct {
	mu                   sync.Mutex
	activeTransports     map[string]*transport    // Tracks created transports for the fuzzer to interact with.
	activeTransportsChan map[string]chan struct{} // Notifies when transport and stream are ready
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		activeTransports:     make(map[string]*transport),
		activeTransportsChan: make(map[string]chan struct{}),
	}
}

// Build creates a new Transport for the given server identifier.
func (b *Builder) Build(serverIdentifier clients.ServerIdentifier) (clients.Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if at, ok := b.activeTransports[serverIdentifier.ServerURI]; ok {
		return at, nil
	}

	streamReadyCh, ok := b.activeTransportsChan[serverIdentifier.ServerURI]
	if !ok {
		streamReadyCh = make(chan struct{})
		b.activeTransportsChan[serverIdentifier.ServerURI] = streamReadyCh
	}

	ft := newTransport(streamReadyCh)
	b.activeTransports[serverIdentifier.ServerURI] = ft
	return ft, nil
}

// Close closes the transport for the given server identifier.
func (b *Builder) Close(serverURI string) {
	b.mu.Lock()
	t, ok := b.activeTransports[serverURI]
	b.mu.Unlock()
	if ok {
		t.Close()
	}
}

// Transport returns the active transport for a given server URI.
func (b *Builder) Transport(ctx context.Context, serverURI string) (*ServerHandle, error) {
	b.mu.Lock()
	if t, ok := b.activeTransports[serverURI]; ok && t.serverHandle() != nil {
		b.mu.Unlock()
		return t.serverHandle(), nil
	}

	ch, ok := b.activeTransportsChan[serverURI]
	if !ok {
		ch = make(chan struct{})
		b.activeTransportsChan[serverURI] = ch
	}
	b.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ch:
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.activeTransports[serverURI].serverHandle(), nil
	}
}

// transport implements clients.Transport.
type transport struct {
	mu              sync.Mutex
	activeADSStream *stream
	closed          *grpcsync.Event
	streamReady     func()
}

func newTransport(streamReady chan struct{}) *transport {
	return &transport{
		closed: grpcsync.NewEvent(),
		streamReady: sync.OnceFunc(func() {
			close(streamReady)
		}),
	}
}

// serverHandle returns a serverhandle for testing.
func (t *transport) serverHandle() *ServerHandle {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.activeADSStream == nil {
		return nil
	}
	return &ServerHandle{fs: t.activeADSStream}
}

// NewStream creates a new stream to the server.
func (t *transport) NewStream(ctx context.Context, _ string) (clients.Stream, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	select {
	case <-t.closed.Done():
		return nil, fmt.Errorf("transport is closed")
	default:
	}

	fs := newStream(ctx, t.closed)
	t.activeADSStream = fs
	t.streamReady()
	return fs, nil
}

// Close closes the stream.
func (t *transport) Close() {
	t.mu.Lock()
	t.closed.Fire()
	stream := t.activeADSStream
	t.mu.Unlock()
	if stream != nil {
		stream.close()
	}
}

// stream implements clients.Stream.
type stream struct {
	ctx    context.Context
	cancel context.CancelFunc
	closed *grpcsync.Event

	reqChan  chan []byte
	respChan chan []byte
}

func newStream(ctx context.Context, closed *grpcsync.Event) *stream {
	c, cancel := context.WithCancel(ctx)
	return &stream{
		ctx:      c,
		cancel:   cancel,
		closed:   closed,
		reqChan:  make(chan []byte),
		respChan: make(chan []byte),
	}
}

// Send sends the provided message on the stream. It puts the request into the
// reqChan for consumption.
func (s *stream) Send(data []byte) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-s.closed.Done():
		return nil
	case s.reqChan <- data:
		return nil
	}
}

// Recv blocks until the next message is received on the stream. It blocks until
// a response is available in the respChan or the context is canceled.
func (s *stream) Recv() ([]byte, error) {
	select {
	case data := <-s.respChan:
		return data, nil
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

// Close closes the stream.
func (s *stream) close() {
	s.cancel()
}

// ServerHandle provides the server-side send/recv methods to interact with
// the stream.
type ServerHandle struct {
	fs *stream
}

// Recv reads the next request from the reqChan. It blocks until a
// request is available or the context expires. It returns an error if the
// context expires or if the request cannot be unmarshaled.
func (h *ServerHandle) Recv(ctx context.Context) (*v3discoverypb.DiscoveryRequest, error) {
	select {
	case data := <-h.fs.reqChan:
		req := &v3discoverypb.DiscoveryRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			return nil, fmt.Errorf("failed to unmarshal request: %v", err)
		}
		return req, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Send simulates a server response. It marshals the provided
// DiscoveryResponse, puts it in the respChan to notify that a response
// is available for the client to Recv.
func (h *ServerHandle) Send(ctx context.Context, resp *v3discoverypb.DiscoveryResponse) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case h.fs.respChan <- data:
		return nil
	}
}
