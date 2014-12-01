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

/*
Package rpc implements various components to perform RPC on top of transport package.
*/
package rpc

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/arcwire-go/transport/transport"
	"golang.org/x/net/context"
)

// DialOption configures how we set up the connection (e.g., the transport
// protocol).
type DialOption func(*ClientConn)

// Dial creates a client connection the given target.
// TODO(zhaoq): Have an option to make Dial return immediately without waiting
// for connection complete.
func Dial(target string, opts ...DialOption) (*ClientConn, error) {
	if target == "" {
		return nil, fmt.Errorf("rpc.Dial: target is empty")
	}
	cc := &ClientConn{
		target:   target,
		Protocol: "http2",
	}
	for _, opt := range opts {
		opt(cc)
	}
	err := cc.resetTransport(false)
	if err != nil {
		return nil, err
	}
	cc.shutdownChan = make(chan struct{})
	// Start to monitor the error status of transport.
	go cc.transportMonitor()
	return cc, nil
}

// ClientConn represents a client connection to an RPC service.
type ClientConn struct {
	target string
	// Protocol is the transport protocol (e.g., HTTP/2) to be used for
	// ClientConn.
	Protocol string
	// UseTLS is true if TLS is used when setting up connection.
	UseTLS bool
	// CAFile is the file name of the CA cert file.
	CAFile       string
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
	for {
		cc.mu.Lock()
		t := cc.transport
		ts := cc.transportSeq
		// Avoid wait() picking up a dying transport unnecessarily.
		cc.transportSeq = 0
		if cc.closing {
			cc.mu.Unlock()
			return fmt.Errorf("rpc.ClientConn.resetTransport: the channel is closing")
		}
		cc.mu.Unlock()
		if closeTransport {
			t.Close()
		}
		newTransport, err := transport.NewClientTransport(cc.Protocol, cc.target, cc.UseTLS, cc.CAFile)
		if err != nil {
			closeTransport = false
			// TODO(zhaoq): Rework once the reconnect spec is ready.
			time.Sleep(1 * time.Second)
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
			err := cc.resetTransport(true)
			if err != nil {
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
			return nil, 0, fmt.Errorf("ClientConn is closing")
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
