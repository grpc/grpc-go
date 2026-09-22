/*
 *
 * Copyright 2022 gRPC authors.
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
	"fmt"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
)

func (s) TestAddGlobalDialOptions(t *testing.T) {
	// Ensure the NewClient fails without credentials
	if _, err := NewClient("fake"); err == nil {
		t.Fatalf("NewClient without a credential did not fail")
	} else {
		if !strings.Contains(err.Error(), "no transport security set") {
			t.Fatalf("NewClient failed with unexpected error: %v", err)
		}
	}

	// Set and check the DialOptions
	opts := []DialOption{WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials())}
	internal.AddGlobalDialOptions.(func(opt ...DialOption))(opts...)
	defer internal.ClearGlobalDialOptions()
	for i, opt := range opts {
		if globalDialOptions[i] != opt {
			t.Fatalf("Unexpected global dial option at index %d: %v != %v", i, globalDialOptions[i], opt)
		}
	}

	// Ensure the NewClient passes with the extra dial options
	if cc, err := NewClient("fake"); err != nil {
		t.Fatalf("NewClient with insecure credential failed: %v", err)
	} else {
		cc.Close()
	}

	internal.ClearGlobalDialOptions()
	if len(globalDialOptions) != 0 {
		t.Fatalf("Unexpected len of globalDialOptions: %d != 0", len(globalDialOptions))
	}
}

// TestDisableGlobalOptions tests dialing with the disableGlobalDialOptions dial
// option. Dialing with this set should not pick up global options.
func (s) TestDisableGlobalOptions(t *testing.T) {
	// Set transport credentials as a global option.
	internal.AddGlobalDialOptions.(func(opt ...DialOption))(WithTransportCredentials(insecure.NewCredentials()))
	defer internal.ClearGlobalDialOptions()
	// Dial with the disable global options dial option. This dial should fail
	// due to the global dial options with credentials not being picked up due
	// to global options being disabled.
	noTSecStr := "no transport security set"
	if _, err := NewClient("fake", internal.DisableGlobalDialOptions.(func() DialOption)()); !strings.Contains(fmt.Sprint(err), noTSecStr) {
		t.Fatalf("NewClient received unexpected error: %v, want error containing %q", err, noTSecStr)
	}
}

type testPerTargetDialOption struct{}

func (do *testPerTargetDialOption) DialOptionForTarget(parsedTarget url.URL) DialOption {
	if parsedTarget.Scheme == "passthrough" {
		return WithTransportCredentials(insecure.NewCredentials()) // credentials provided, should pass NewClient.
	}
	return EmptyDialOption{} // no credentials, should fail NewClient
}

// TestGlobalPerTargetDialOption configures a global per target dial option that
// produces transport credentials for channels using "passthrough" scheme.
// Channels that use the passthrough scheme should be successfully created due
// to picking up transport credentials, whereas other channels should fail at
// creation due to not having transport credentials.
func (s) TestGlobalPerTargetDialOption(t *testing.T) {
	internal.AddGlobalPerTargetDialOptions.(func(opt any))(&testPerTargetDialOption{})
	defer internal.ClearGlobalPerTargetDialOptions()
	noTSecStr := "no transport security set"
	if _, err := NewClient("dns:///fake"); !strings.Contains(fmt.Sprint(err), noTSecStr) {
		t.Fatalf("NewClient received unexpected error: %v, want error containing %q", err, noTSecStr)
	}
	cc, err := NewClient("passthrough:///nice")
	if err != nil {
		t.Fatalf("NewClient with insecure credentials failed: %v", err)
	}
	cc.Close()
}

func (s) TestAddGlobalServerOptions(t *testing.T) {
	const maxRecvSize = 998765
	// Set and check the ServerOptions
	opts := []ServerOption{Creds(insecure.NewCredentials()), MaxRecvMsgSize(maxRecvSize)}
	internal.AddGlobalServerOptions.(func(opt ...ServerOption))(opts...)
	defer internal.ClearGlobalServerOptions()
	for i, opt := range opts {
		if globalServerOptions[i] != opt {
			t.Fatalf("Unexpected global server option at index %d: %v != %v", i, globalServerOptions[i], opt)
		}
	}

	// Ensure the extra server options applies to new servers
	s := NewServer()
	if s.opts.maxReceiveMessageSize != maxRecvSize {
		t.Fatalf("Unexpected s.opts.maxReceiveMessageSize: %d != %d", s.opts.maxReceiveMessageSize, maxRecvSize)
	}

	internal.ClearGlobalServerOptions()
	if len(globalServerOptions) != 0 {
		t.Fatalf("Unexpected len of globalServerOptions: %d != 0", len(globalServerOptions))
	}
}

// TestJoinDialOption tests the join dial option. It configures a joined dial
// option with three individual dial options, and verifies that all three are
// successfully applied.
func (s) TestJoinDialOption(t *testing.T) {
	const maxRecvSize = 998765
	const initialWindowSize = 100
	jdo := newJoinDialOption(WithTransportCredentials(insecure.NewCredentials()), WithReadBufferSize(maxRecvSize), WithInitialWindowSize(initialWindowSize))
	cc, err := NewClient("fake", jdo)
	if err != nil {
		t.Fatalf("NewClient with insecure credentials failed: %v", err)
	}
	defer cc.Close()
	if cc.dopts.copts.ReadBufferSize != maxRecvSize {
		t.Fatalf("Unexpected cc.dopts.copts.ReadBufferSize: %d != %d", cc.dopts.copts.ReadBufferSize, maxRecvSize)
	}
	if cc.dopts.copts.InitialWindowSize != initialWindowSize {
		t.Fatalf("Unexpected cc.dopts.copts.InitialWindowSize: %d != %d", cc.dopts.copts.InitialWindowSize, initialWindowSize)
	}
}

// TestJoinServerOption tests the join server option. It configures a joined
// server option with three individual server options, and verifies that all
// three are successfully applied.
func (s) TestJoinServerOption(t *testing.T) {
	const maxRecvSize = 998765
	const initialWindowSize = 100
	jso := newJoinServerOption(Creds(insecure.NewCredentials()), MaxRecvMsgSize(maxRecvSize), InitialWindowSize(initialWindowSize))
	s := NewServer(jso)
	if s.opts.maxReceiveMessageSize != maxRecvSize {
		t.Fatalf("Unexpected s.opts.maxReceiveMessageSize: %d != %d", s.opts.maxReceiveMessageSize, maxRecvSize)
	}
	if s.opts.initialWindowSize != initialWindowSize {
		t.Fatalf("Unexpected s.opts.initialWindowSize: %d != %d", s.opts.initialWindowSize, initialWindowSize)
	}
}

// funcTestHeaderListSizeDialOptionServerOption tests
func (s) TestHeaderListSizeDialOptionServerOption(t *testing.T) {
	const maxHeaderListSize uint32 = 998765
	clientHeaderListSize := WithMaxHeaderListSize(maxHeaderListSize)
	if clientHeaderListSize.(MaxHeaderListSizeDialOption).MaxHeaderListSize != maxHeaderListSize {
		t.Fatalf("Unexpected s.opts.MaxHeaderListSizeDialOption.MaxHeaderListSize: %d != %d", clientHeaderListSize, maxHeaderListSize)
	}
	serverHeaderListSize := MaxHeaderListSize(maxHeaderListSize)
	if serverHeaderListSize.(MaxHeaderListSizeServerOption).MaxHeaderListSize != maxHeaderListSize {
		t.Fatalf("Unexpected s.opts.MaxHeaderListSizeDialOption.MaxHeaderListSize: %d != %d", serverHeaderListSize, maxHeaderListSize)
	}
}

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
