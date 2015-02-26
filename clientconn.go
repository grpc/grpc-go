/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"errors"
	"log"
	"sync"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/transport"
)

var (
	// ErrUnspecTarget indicates that the target address is unspecified.
	ErrUnspecTarget = errors.New("grpc: target is unspecified")
	// ErrClientConnClosing indicates that the operation is illegal because
	// the session is closing.
	ErrClientConnClosing = errors.New("grpc: the client connection is closing")
)

type dialOptions struct {
	protocol    string
	authOptions []credentials.Credentials
}

// DialOption configures how we set up the connection including auth
// credentials.
type DialOption func(*dialOptions)

// WithTransportCredentials returns a DialOption which configures a
// connection level security credentials (e.g., TLS/SSL).
func WithTransportCredentials(creds credentials.TransportAuthenticator) DialOption {
	return func(o *dialOptions) {
		o.authOptions = append(o.authOptions, creds)
	}
}

// WithPerRPCCredentials returns a DialOption which sets
// credentials which will place auth state on each outbound RPC.
func WithPerRPCCredentials(creds credentials.Credentials) DialOption {
	return func(o *dialOptions) {
		o.authOptions = append(o.authOptions, creds)
	}
}

// Dial creates a client connection the given target.
// TODO(zhaoq): Have an option to make Dial return immediately without waiting
// for connection to complete.
func Dial(target string, opts ...DialOption) (*ClientConn, error) {
	if target == "" {
		return nil, ErrUnspecTarget
	}
	cc := &ClientConn{
		target: target,
	}
	for _, opt := range opts {
		opt(&cc.dopts)
	}
	if err := cc.resetTransport(false); err != nil {
		return nil, err
	}
	cc.shutdownChan = make(chan struct{})
	// Start to monitor the error status of transport.
	go cc.transportMonitor()
	return cc, nil
}

// ClientConn represents a client connection to an RPC service.
type ClientConn struct {
	target       string
	dopts        dialOptions
	shutdownChan chan struct{}

	mu sync.Mutex
	// Is closed and becomes nil when a new transport is up.
	ready chan struct{}
	// Indicates the ClientConn is under destruction.
	closing bool
	// Every time a new transport is created, this is incremented by 1. Used
	// to avoid trying to recreate a transport while the new one is already
	// under construction.
	transportSeq int
	transport    transport.ClientTransport
}

func (cc *ClientConn) resetTransport(closeTransport bool) error {
	var retries int
	for {
		cc.mu.Lock()
		t := cc.transport
		ts := cc.transportSeq
		// Avoid wait() picking up a dying transport unnecessarily.
		cc.transportSeq = 0
		if cc.closing {
			cc.mu.Unlock()
			return ErrClientConnClosing
		}
		cc.mu.Unlock()
		if closeTransport {
			t.Close()
		}
		newTransport, err := transport.NewClientTransport(cc.dopts.protocol, cc.target, cc.dopts.authOptions)
		if err != nil {
			// TODO(zhaoq): Record the error with glog.V.
			closeTransport = false
			time.Sleep(backoff(retries))
			retries++
			log.Printf("grpc: ClientConn.resetTransport failed to create client transport: %v; Reconnecting to %q", err, cc.target)
			continue
		}
		cc.mu.Lock()
		cc.transport = newTransport
		cc.transportSeq = ts + 1
		if cc.ready != nil {
			close(cc.ready)
			cc.ready = nil
		}
		cc.mu.Unlock()
		return nil
	}
}

// Run in a goroutine to track the error in transport and create the
// new transport if an error happens. It returns when the channel is closing.
func (cc *ClientConn) transportMonitor() {
	for {
		select {
		// shutdownChan is needed to detect the channel teardown when
		// the ClientConn is idle (i.e., no RPC in flight).
		case <-cc.shutdownChan:
			return
		case <-cc.transport.Error():
			if err := cc.resetTransport(true); err != nil {
				// The channel is closing.
				return
			}
			continue
		}
	}
}

// When wait returns, either the new transport is up or ClientConn is
// closing. Used to avoid working on a dying transport. It updates and
// returns the transport and its version when there is no error.
func (cc *ClientConn) wait(ctx context.Context, ts int) (transport.ClientTransport, int, error) {
	for {
		cc.mu.Lock()
		switch {
		case cc.closing:
			cc.mu.Unlock()
			return nil, 0, ErrClientConnClosing
		case ts < cc.transportSeq:
			// Worked on a dying transport. Try the new one immediately.
			defer cc.mu.Unlock()
			return cc.transport, cc.transportSeq, nil
		default:
			ready := cc.ready
			if ready == nil {
				ready = make(chan struct{})
				cc.ready = ready
			}
			cc.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, 0, transport.ContextErr(ctx.Err())
			// Wait until the new transport is ready.
			case <-ready:
			}
		}
	}
}

// Close starts to tear down the ClientConn.
// TODO(zhaoq): Make this synchronous to avoid unbounded memory consumption in
// some edge cases (e.g., the caller opens and closes many ClientConn's in a
// tight loop.
func (cc *ClientConn) Close() {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closing {
		return
	}
	cc.closing = true
	cc.transport.Close()
	close(cc.shutdownChan)
}
