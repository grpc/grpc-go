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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
)

// TestChildChannelOptions_Client tests WithChildChannelOptions on client side.
func (s) TestChildChannelOptions_Client(t *testing.T) {
	const (
		initReadBufferSize        = 512
		overwrittenReadBufferSize = 1024
		writeBufferSize           = 2048
	)

	// Test multiple WithChildChannelOptions calls: the last call replaces earlier ones.
	cc, err := NewClient("passthrough:///test",
		WithTransportCredentials(insecure.NewCredentials()),
		WithChildChannelOptions(WithInitialWindowSize(4096)),
		WithChildChannelOptions(
			WithReadBufferSize(initReadBufferSize),
			WithWriteBufferSize(writeBufferSize),
			WithReadBufferSize(overwrittenReadBufferSize),
		),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer cc.Close()

	// Verify that parent options are not modified by child options.
	if got, want := cc.dopts.copts.ReadBufferSize, defaultReadBufSize; got != want {
		t.Fatalf("Parent cc.dopts.copts.ReadBufferSize = %d, want %d", got, want)
	}
	if got, want := cc.dopts.copts.WriteBufferSize, defaultWriteBufSize; got != want {
		t.Fatalf("Parent cc.dopts.copts.WriteBufferSize = %d, want %d", got, want)
	}
	if got, want := cc.dopts.copts.InitialWindowSize, int32(0); got != want {
		t.Fatalf("Parent cc.dopts.copts.InitialWindowSize = %d, want %d", got, want)
	}

	// Verify that child dial options from the last call are stored in cc.dopts in order.
	if got, want := len(cc.dopts.childDialOptions), 3; got != want {
		t.Fatalf("Child dial options count = %d, want %d", got, want)
	}
	cc.dopts.childDialOptions[0].apply(&cc.dopts)
	if got, want := cc.dopts.copts.ReadBufferSize, initReadBufferSize; got != want {
		t.Fatalf("Child dial option[0] ReadBufferSize = %d, want %d", got, want)
	}
	cc.dopts.childDialOptions[1].apply(&cc.dopts)
	if got, want := cc.dopts.copts.WriteBufferSize, writeBufferSize; got != want {
		t.Fatalf("Child dial option[1] WriteBufferSize = %d, want %d", got, want)
	}
	cc.dopts.childDialOptions[2].apply(&cc.dopts)
	if got, want := cc.dopts.copts.ReadBufferSize, overwrittenReadBufferSize; got != want {
		t.Fatalf("Child dial option[2] ReadBufferSize = %d, want %d", got, want)
	}

	// Verify that overridden child dial options were not applied.
	if got, want := cc.dopts.copts.InitialWindowSize, int32(0); got != want {
		t.Fatalf("Child InitialWindowSize = %d, want %d", got, want)
	}
}

// TestChildChannelOptions_Server tests ChildChannelOptions on server side.
func (s) TestChildChannelOptions_Server(t *testing.T) {
	const (
		initReadBufferSize        = 512
		overwrittenReadBufferSize = 1024
		writeBufferSize           = 2048
	)

	srv := NewServer(
		ChildChannelOptions(WithInitialWindowSize(4096)),
		ChildChannelOptions(
			WithReadBufferSize(initReadBufferSize),
			WithWriteBufferSize(writeBufferSize),
			WithReadBufferSize(overwrittenReadBufferSize),
		),
	)
	defer srv.Stop()

	// Verify that child dial options from the last call are stored in srv.opts.
	if got, want := len(srv.opts.childDialOptions), 3; got != want {
		t.Fatalf("Child dial options count = %d, want %d", got, want)
	}

	// Verify that internal.ChildDialOptionsFromServer returns the options in order.
	childOpts := internal.ChildDialOptionsFromServer.(func(*Server) []DialOption)(srv)
	if got, want := len(childOpts), 3; got != want {
		t.Fatalf("Child dial options count from server accessor = %d, want %d", got, want)
	}
	var dopts dialOptions
	childOpts[0].apply(&dopts)
	if got, want := dopts.copts.ReadBufferSize, initReadBufferSize; got != want {
		t.Fatalf("Child dial option[0] ReadBufferSize = %d, want %d", got, want)
	}
	childOpts[1].apply(&dopts)
	if got, want := dopts.copts.WriteBufferSize, writeBufferSize; got != want {
		t.Fatalf("Child dial option[1] WriteBufferSize = %d, want %d", got, want)
	}
	childOpts[2].apply(&dopts)
	if got, want := dopts.copts.ReadBufferSize, overwrittenReadBufferSize; got != want {
		t.Fatalf("Child dial option[2] ReadBufferSize = %d, want %d", got, want)
	}

	// Verify that overridden child dial options were not applied.
	if got, want := dopts.copts.InitialWindowSize, int32(0); got != want {
		t.Fatalf("Child InitialWindowSize = %d, want %d", got, want)
	}
}

// TestChildChannelOptions_Isolation tests that interceptors passed in child
// options do not execute on the parent channel.
func (s) TestChildChannelOptions_Isolation(t *testing.T) {
	var parentClientInterceptorCalled, childClientInterceptorCalled bool
	parentClientInt := func(ctx context.Context, method string, req, reply any, cc *ClientConn, invoker UnaryInvoker, opts ...CallOption) error {
		parentClientInterceptorCalled = true
		return invoker(ctx, method, req, reply, cc, opts...)
	}
	childClientInt := func(ctx context.Context, method string, req, reply any, cc *ClientConn, invoker UnaryInvoker, opts ...CallOption) error {
		childClientInterceptorCalled = true
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	cc, err := NewClient("passthrough:///test",
		WithTransportCredentials(insecure.NewCredentials()),
		WithUnaryInterceptor(parentClientInt),
		WithChildChannelOptions(WithUnaryInterceptor(childClientInt)),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer cc.Close()

	if cc.dopts.unaryInt == nil {
		t.Fatalf("Parent cc.dopts.unaryInt is nil, want parent interceptor")
	}

	// Make an invocation to check interceptor execution on parent channel.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_ = cc.Invoke(ctx, "/test/method", nil, nil)
	if !parentClientInterceptorCalled {
		t.Errorf("Parent client interceptor was not called")
	}
	if childClientInterceptorCalled {
		t.Errorf("Child client interceptor was unexpectedly called on parent channel call")
	}
}
