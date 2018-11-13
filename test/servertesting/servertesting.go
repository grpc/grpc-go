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

// Package servertesting provides helpers for testing gRPC service
// implementations over the full gRPC client-server stack.
package servertesting

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

// A Tester wraps a grpc.Server, with connections made over a bufconn.Listener.
type Tester struct {
	listener *bufconn.Listener
	server   *grpc.Server
}

// New returns a new Tester. Close() must be called to clean up, even if Serve()
// is never called.
func New(opts ...grpc.ServerOption) *Tester {
	return &Tester{
		// 1MB is entirely arbitrary
		listener: bufconn.Listen(1 << 20),
		server:   grpc.NewServer(opts...),
	}
}

// RegisterService registers a service implementation with the underlying
// grpc.Server. The registerFunc argument must be a RegisterXServer() function
// from a proto file, where X is the name of some service. The implementation
// argument must implement the respective XServer interface.
//
// If only one service needs to be registered, rather use NewClientConn().
func (t *Tester) RegisterService(registerFunc, implementation interface{}) (err error) {
	// In case we haven't covered all bases in the argument checks, rather don't
	// panic as it's a recoverable issue. The intention is that this package
	// will be used in tests, so it's preferable that we have clean error
	// reporting.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("recovered panic: %v", r)
		}
	}()

	if isNil(registerFunc) {
		return errors.New("nil registering function")
	}
	if isNil(implementation) {
		return errors.New("nil implementation")
	}

	typ := reflect.TypeOf(registerFunc)
	if got, want := typ.Kind(), reflect.Func; got != want {
		return fmt.Errorf("got first argument of kind %s; must be a RegisterXServiceServer function", got)
	}
	if got, want := typ.NumIn(), 2; got != want {
		return fmt.Errorf("registering function accepts %d input arguments; expecting %d", got, want)
	}
	if got, want := typ.NumOut(), 0; got != want {
		return fmt.Errorf("registering function returns %d arguments; expecting %d", got, want)
	}

	args := []reflect.Value{
		reflect.ValueOf(t.server),
		reflect.ValueOf(implementation),
	}
	errFormats := []string{
		"registering function accepts %s as first argument; must be able to assign gRPC server of type %s",
		"registering function accepts %s as second argument; must be able to assign received implementation of type %s",
	}
	for i, arg := range args {
		if got, argT := typ.In(i), arg.Type(); !argT.AssignableTo(got) {
			return fmt.Errorf(errFormats[i], got, argT)
		}
	}

	reflect.ValueOf(registerFunc).Call(args)
	return nil
}

// isNil returns true if the value of v is nil, regardless of type.
func isNil(v interface{}) bool {
	if v == nil {
		return true
	}
	switch reflect.TypeOf(v).Kind() {
	case reflect.Chan:
	case reflect.Func:
	case reflect.Interface:
	case reflect.Map:
	case reflect.Ptr:
	case reflect.Slice:
	default:
		return false
	}
	return reflect.ValueOf(v).IsNil()
}

// Serve is equivalent to grpc.Server.Serve() but without the need for a
// Listener.
func (t *Tester) Serve() {
	t.server.Serve(t.listener)
}

// Close gracefully stops the grpc.Server if Serve was called, and closes the
// Listener. It must be called even if Serve() was not.
func (t *Tester) Close() error {
	t.server.GracefulStop()
	return t.listener.Close()
}

// Dial calls grpc.Dial() with a dialer that will connect to the underlying
// bufconn.Listener with the provided options, which must not include a dialer
// themselves. All connections use a grpc.WithInsecure() option.
func (t *Tester) Dial(opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial("", t.dialOpts(opts...)...)
}

// dialOpts extends user-provided options with those necessary to connect to the
// grpc.Server.
func (t *Tester) dialOpts(user ...grpc.DialOption) []grpc.DialOption {
	return append(user,
		grpc.WithDialer(func(_ string, _ time.Duration) (net.Conn, error) {
			return t.listener.Dial()
		}),
		grpc.WithInsecure(),
	)
}

// NewClientConn is a convenience wrapper for registering a single service with
// a Tester, and returning the ClientConn obtained from Dial(). The returned
// cleanup function blocks until the underlying grpc.Server stops, and does not
// need to be called if NewClientConn returns an error.
//
// See Tester.RegisterService() for a description of the arguments.
func NewClientConn(registerFunc, serviceImplementation interface{}, opts ...grpc.ServerOption) (*grpc.ClientConn, func(), error) {
	t := New(opts...)
	if err := t.RegisterService(registerFunc, serviceImplementation); err != nil {
		t.Close()
		return nil, func() {}, fmt.Errorf("registering service: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		t.Serve()
	}()

	cleanup := func() {
		t.Close()
		<-done
	}

	conn, err := t.Dial()
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("dialing server: %v", err)
	}

	return conn, cleanup, nil
}
