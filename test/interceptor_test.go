/*
 *
 * Copyright 2022 gRPC authors.

 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

type parentCtxkey struct{}
type firstInterceptorCtxkey struct{}
type secondInterceptorCtxkey struct{}
type baseInterceptorCtxKey struct{}

const (
	parentCtxVal            = "parent"
	firstInterceptorCtxVal  = "firstInterceptor"
	secondInterceptorCtxVal = "secondInterceptor"
	baseInterceptorCtxVal   = "baseInterceptor"
)

// TestUnaryClientInterceptor_ContextValuePropagation verifies that a unary
// interceptor receives context values specified in the context passed to the
// RPC call.
func (s) TestUnaryClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	unaryInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.Send(fmt.Errorf("unaryInt got %q in context.Val, want %q", got, parentCtxVal))
		}
		errCh.Send(nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server and use the above unary interceptor while creating a
	// ClientConn to it.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil, grpc.WithUnaryInterceptor(unaryInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.EmptyCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestChainUnaryClientInterceptor_ContextValuePropagation verifies that a chain
// of unary interceptors receive context values specified in the original call
// as well as the ones specified by prior interceptors in the chain.
func (s) TestChainUnaryClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	firstInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(firstInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", firstInterceptorCtxkey{}))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		firstCtx := context.WithValue(ctx, firstInterceptorCtxkey{}, firstInterceptorCtxVal)
		return invoker(firstCtx, method, req, reply, cc, opts...)
	}

	secondInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		secondCtx := context.WithValue(ctx, secondInterceptorCtxkey{}, secondInterceptorCtxVal)
		return invoker(secondCtx, method, req, reply, cc, opts...)
	}

	lastInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if got, ok := ctx.Value(secondInterceptorCtxkey{}).(string); !ok || got != secondInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, secondInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server and use the above chain of interceptors while creating
	// a ClientConn to it.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil, grpc.WithChainUnaryInterceptor(firstInt, secondInt, lastInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.EmptyCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestChainOnBaseUnaryClientInterceptor_ContextValuePropagation verifies that
// unary interceptors specified as a base interceptor or as a chain interceptor
// receive context values specified in the original call as well as the ones
// specified by interceptors in the chain.
func (s) TestChainOnBaseUnaryClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	baseInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("base interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(baseInterceptorCtxKey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("baseinterceptor should not have %T in context", baseInterceptorCtxKey{}))
		}
		baseCtx := context.WithValue(ctx, baseInterceptorCtxKey{}, baseInterceptorCtxVal)
		return invoker(baseCtx, method, req, reply, cc, opts...)
	}

	chainInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("chain interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(baseInterceptorCtxKey{}).(string); !ok || got != baseInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("chain interceptor got %q in context.Val, want %q", got, baseInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server and use the above chain of interceptors while creating
	// a ClientConn to it.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil, grpc.WithUnaryInterceptor(baseInt), grpc.WithChainUnaryInterceptor(chainInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.EmptyCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestChainStreamClientInterceptor_ContextValuePropagation verifies that a
// chain of stream interceptors receive context values specified in the original
// call as well as the ones specified by the prior interceptors in the chain.
func (s) TestChainStreamClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	firstInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(firstInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", firstInterceptorCtxkey{}))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		firstCtx := context.WithValue(ctx, firstInterceptorCtxkey{}, firstInterceptorCtxVal)
		return streamer(firstCtx, desc, cc, method, opts...)
	}

	secondInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		secondCtx := context.WithValue(ctx, secondInterceptorCtxkey{}, secondInterceptorCtxVal)
		return streamer(secondCtx, desc, cc, method, opts...)
	}

	lastInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if got, ok := ctx.Value(secondInterceptorCtxkey{}).(string); !ok || got != secondInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, secondInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return streamer(ctx, desc, cc, method, opts...)
	}

	// Start a stub server and use the above chain of interceptors while creating
	// a ClientConn to it.
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
	}
	if err := ss.Start(nil, grpc.WithChainStreamInterceptor(firstInt, secondInt, lastInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.FullDuplexCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal)); err != nil {
		t.Fatalf("ss.Client.FullDuplexCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for stream interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("stream interceptor failed: %v", val)
	}
}

// TestCallUnaryClientInterceptor_ContextValuePropagation verifies that unary interceptors
// passed as CallOptions receive context values specified in the original call
// as well as the ones specified by prior interceptors in the chain.
func (s) TestCallUnaryClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	firstInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(firstInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", firstInterceptorCtxkey{}))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		firstCtx := context.WithValue(ctx, firstInterceptorCtxkey{}, firstInterceptorCtxVal)
		return invoker(firstCtx, method, req, reply, cc, opts...)
	}

	secondInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		secondCtx := context.WithValue(ctx, secondInterceptorCtxkey{}, secondInterceptorCtxVal)
		return invoker(secondCtx, method, req, reply, cc, opts...)
	}

	lastInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if got, ok := ctx.Value(secondInterceptorCtxkey{}).(string); !ok || got != secondInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, secondInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	opts := []grpc.CallOption{
		grpc.PerRPCUnaryClientInterceptor(firstInt),
		grpc.PerRPCUnaryClientInterceptor(secondInt),
		grpc.PerRPCUnaryClientInterceptor(lastInt),
	}
	// Use the above chain of interceptors while invoking a unary RPC.
	if _, err := ss.Client.EmptyCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), &testpb.Empty{}, opts...); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestCallOnDialUnaryClientInterceptor_ContextValuePropagation verifies that
// unary interceptors specified as DialOptions or as CallOptions
// receive context values specified in the original call as well as the ones
// specified by interceptors in the chain.
func (s) TestCallOnDialUnaryClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	dialInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("dial interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(baseInterceptorCtxKey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("dial interceptor should not have %T in context", baseInterceptorCtxKey{}))
		}
		dialCtx := context.WithValue(ctx, baseInterceptorCtxKey{}, baseInterceptorCtxVal)
		return invoker(dialCtx, method, req, reply, cc, opts...)
	}

	callInt := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("call interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(baseInterceptorCtxKey{}).(string); !ok || got != baseInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("call interceptor got %q in context.Val, want %q", got, baseInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server and use the first unary interceptor while creating
	// a ClientConn to it.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil, grpc.WithUnaryInterceptor(dialInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Use the second interceptor while invoking a unary RPC.
	if _, err := ss.Client.EmptyCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), &testpb.Empty{}, grpc.PerRPCUnaryClientInterceptor(callInt)); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestCallStreamClientInterceptor_ContextValuePropagation verifies that stream interceptors
// passed as CallOptions receive context values specified in the original
// call as well as the ones specified by the prior interceptors in the chain.
func (s) TestCallStreamClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	firstInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(firstInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", firstInterceptorCtxkey{}))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		firstCtx := context.WithValue(ctx, firstInterceptorCtxkey{}, firstInterceptorCtxVal)
		return streamer(firstCtx, desc, cc, method, opts...)
	}

	secondInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if ctx.Value(secondInterceptorCtxkey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor should not have %T in context", secondInterceptorCtxkey{}))
		}
		secondCtx := context.WithValue(ctx, secondInterceptorCtxkey{}, secondInterceptorCtxVal)
		return streamer(secondCtx, desc, cc, method, opts...)
	}

	lastInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(firstInterceptorCtxkey{}).(string); !ok || got != firstInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, firstInterceptorCtxVal))
		}
		if got, ok := ctx.Value(secondInterceptorCtxkey{}).(string); !ok || got != secondInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %q in context.Val, want %q", got, secondInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return streamer(ctx, desc, cc, method, opts...)
	}

	// Start a stub server.
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	opts := []grpc.CallOption{
		grpc.PerRPCStreamClientInterceptor(firstInt),
		grpc.PerRPCStreamClientInterceptor(secondInt),
		grpc.PerRPCStreamClientInterceptor(lastInt),
	}
	// Use the above chain of interceptors while invoking a streaming RPC.
	if _, err := ss.Client.FullDuplexCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), opts...); err != nil {
		t.Fatalf("ss.Client.FullDuplexCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for stream interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("stream interceptor failed: %v", val)
	}
}

// TestCallOnDialStreamClientInterceptor_ContextValuePropagation verifies that stream interceptors
// specified as DialOptions or as CallOptions receive context values specified in the original call
// as well as the ones specified by interceptors in the chain.
func (s) TestCallOnDialStreamClientInterceptor_ContextValuePropagation(t *testing.T) {
	errCh := testutils.NewChannel()
	dialInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("dial interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if ctx.Value(baseInterceptorCtxKey{}) != nil {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor should not have %T in context", baseInterceptorCtxKey{}))
		}
		dialCtx := context.WithValue(ctx, baseInterceptorCtxKey{}, baseInterceptorCtxVal)
		return streamer(dialCtx, desc, cc, method, opts...)
	}

	callInt := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if got, ok := ctx.Value(parentCtxkey{}).(string); !ok || got != parentCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("call interceptor got %q in context.Val, want %q", got, parentCtxVal))
		}
		if got, ok := ctx.Value(baseInterceptorCtxKey{}).(string); !ok || got != baseInterceptorCtxVal {
			errCh.SendContext(ctx, fmt.Errorf("call interceptor got %q in context.Val, want %q", got, baseInterceptorCtxVal))
		}
		errCh.SendContext(ctx, nil)
		return streamer(ctx, desc, cc, method, opts...)
	}

	// Start a stub server and use the first stream interceptor while creating
	// a ClientConn to it.
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
	}
	if err := ss.Start(nil, grpc.WithStreamInterceptor(dialInt)); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Use the second interceptor while invoking a streaming RPC.
	if _, err := ss.Client.FullDuplexCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), grpc.PerRPCStreamClientInterceptor(callInt)); err != nil {
		t.Fatalf("ss.Client.FullDuplexCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for stream interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("stream interceptor failed: %v", val)
	}
}

// TestCallUnaryClientInterceptor_PredecessorsFiltering verifies that unary interceptors
// passed as CallOptions can see only its successors, not itself and its predecessors.
func (s) TestCallUnaryClientInterceptor_PredecessorsFiltering(t *testing.T) {
	errCh := testutils.NewChannel()
	var firstInt, secondInt, lastInt grpc.UnaryClientInterceptor
	firstInt = func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if len(opts) != 2 {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %d CallOption(s), want 2", len(opts)))
		} else {
			if opt, ok := opts[0].(grpc.UnaryClientInterceptorCallOption); !ok || reflect.ValueOf(opt.UnaryClientInterceptor).Pointer() != reflect.ValueOf(secondInt).Pointer() {
				errCh.SendContext(ctx, errors.New("first interceptor should see second interceptor in CallOptions"))
			}
			if opt, ok := opts[1].(grpc.UnaryClientInterceptorCallOption); !ok || reflect.ValueOf(opt.UnaryClientInterceptor).Pointer() != reflect.ValueOf(lastInt).Pointer() {
				errCh.SendContext(ctx, errors.New("first interceptor should see last interceptor in CallOptions"))
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	secondInt = func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if len(opts) != 1 {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %d CallOption(s), want 1", len(opts)))
		} else {
			if opt, ok := opts[0].(grpc.UnaryClientInterceptorCallOption); !ok || reflect.ValueOf(opt.UnaryClientInterceptor).Pointer() != reflect.ValueOf(lastInt).Pointer() {
				errCh.SendContext(ctx, errors.New("second interceptor should see last interceptor in CallOptions"))
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	lastInt = func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if len(opts) != 0 {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %d CallOption(s), want 0", len(opts)))
		}
		errCh.SendContext(ctx, nil)
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	// Start a stub server.
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) { return &testpb.Empty{}, nil },
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	opts := []grpc.CallOption{
		grpc.PerRPCUnaryClientInterceptor(firstInt),
		grpc.PerRPCUnaryClientInterceptor(secondInt),
		grpc.PerRPCUnaryClientInterceptor(lastInt),
	}
	// Use the above chain of interceptors while invoking a unary RPC.
	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, opts...); err != nil {
		t.Fatalf("ss.Client.EmptyCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for unary interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("unary interceptor failed: %v", val)
	}
}

// TestCallStreamClientInterceptor_PredecessorsFiltering verifies that unary interceptors
// passed as CallOptions can see only its successors, not itself and its predecessors.
func (s) TestCallStreamClientInterceptor_PredecessorsFiltering(t *testing.T) {
	errCh := testutils.NewChannel()
	var firstInt, secondInt, lastInt grpc.StreamClientInterceptor
	firstInt = func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if len(opts) != 2 {
			errCh.SendContext(ctx, fmt.Errorf("first interceptor got %d CallOption(s), want 2", len(opts)))
		} else {
			if opt, ok := opts[0].(grpc.StreamClientInterceptorCallOption); !ok || reflect.ValueOf(opt.StreamClientInterceptor).Pointer() != reflect.ValueOf(secondInt).Pointer() {
				errCh.SendContext(ctx, errors.New("first interceptor should see second interceptor in CallOptions"))
			}
			if opt, ok := opts[1].(grpc.StreamClientInterceptorCallOption); !ok || reflect.ValueOf(opt.StreamClientInterceptor).Pointer() != reflect.ValueOf(lastInt).Pointer() {
				errCh.SendContext(ctx, errors.New("first interceptor should see last interceptor in CallOptions"))
			}
		}
		return streamer(ctx, desc, cc, method, opts...)
	}

	secondInt = func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if len(opts) != 1 {
			errCh.SendContext(ctx, fmt.Errorf("second interceptor got %d CallOption(s), want 1", len(opts)))
		} else {
			if opt, ok := opts[0].(grpc.StreamClientInterceptorCallOption); !ok || reflect.ValueOf(opt.StreamClientInterceptor).Pointer() != reflect.ValueOf(lastInt).Pointer() {
				errCh.SendContext(ctx, errors.New("second interceptor should see last interceptor in CallOptions"))
			}
		}
		return streamer(ctx, desc, cc, method, opts...)
	}

	lastInt = func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		if len(opts) != 0 {
			errCh.SendContext(ctx, fmt.Errorf("last interceptor got %d CallOption(s), want 0", len(opts)))
		}
		errCh.SendContext(ctx, nil)
		return streamer(ctx, desc, cc, method, opts...)
	}

	// Start a stub server.
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			if _, err := stream.Recv(); err != nil {
				return err
			}
			return stream.Send(&testpb.StreamingOutputCallResponse{})
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Failed to start stub server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	opts := []grpc.CallOption{
		grpc.PerRPCStreamClientInterceptor(firstInt),
		grpc.PerRPCStreamClientInterceptor(secondInt),
		grpc.PerRPCStreamClientInterceptor(lastInt),
	}
	// Use the above chain of interceptors while invoking a streaming RPC.
	if _, err := ss.Client.FullDuplexCall(context.WithValue(ctx, parentCtxkey{}, parentCtxVal), opts...); err != nil {
		t.Fatalf("ss.Client.FullDuplexCall() failed: %v", err)
	}
	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("timeout when waiting for stream interceptor to be invoked: %v", err)
	}
	if val != nil {
		t.Fatalf("stream interceptor failed: %v", val)
	}
}
