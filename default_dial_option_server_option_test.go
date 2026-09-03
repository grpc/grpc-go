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
	const readBufferSize = 1024
	const writeBufferSize = 2048
	const initialWindowSize = 4096
	const wantChildOptsCount = 3

	opt1 := WithReadBufferSize(readBufferSize)
	opt2 := WithWriteBufferSize(writeBufferSize)
	opt3 := WithInitialWindowSize(initialWindowSize)

	// Test multiple WithChildChannelOptions calls.
	cc, err := NewClient("passthrough:///test",
		WithTransportCredentials(insecure.NewCredentials()),
		WithChildChannelOptions(opt1, opt2),
		WithChildChannelOptions(opt3),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer cc.Close()

	// Verify that child dial options are stored in cc.dopts in order.
	if len(cc.dopts.childDialOptions) != wantChildOptsCount {
		t.Fatalf("Child dial options count = %d, want %d", len(cc.dopts.childDialOptions), wantChildOptsCount)
	}

	// Verify that parent options are not modified by child options.
	if cc.dopts.copts.ReadBufferSize == readBufferSize {
		t.Fatalf("Parent cc.dopts.copts.ReadBufferSize was modified by child option: got %d, want default", readBufferSize)
	}
	if cc.dopts.copts.WriteBufferSize == writeBufferSize {
		t.Fatalf("Parent cc.dopts.copts.WriteBufferSize was modified by child option: got %d, want default", writeBufferSize)
	}
	if cc.dopts.copts.InitialWindowSize == initialWindowSize {
		t.Fatalf("Parent cc.dopts.copts.InitialWindowSize was modified by child option: got %d, want default", initialWindowSize)
	}
}

// TestChildChannelOptions_Server tests ChildChannelOptions on server side.
func (s) TestChildChannelOptions_Server(t *testing.T) {
	const readBufferSize = 1024
	const writeBufferSize = 2048
	const wantChildOptsCount = 2

	opt1 := WithReadBufferSize(readBufferSize)
	opt2 := WithWriteBufferSize(writeBufferSize)

	srv := NewServer(ChildChannelOptions(opt1), ChildChannelOptions(opt2))
	defer srv.Stop()

	// Verify that child dial options are stored in srv.opts in order.
	if len(srv.opts.childDialOptions) != wantChildOptsCount {
		t.Fatalf("Child dial options count = %d, want %d", len(srv.opts.childDialOptions), wantChildOptsCount)
	}

	// Verify that internal.ChildDialOptionsFromServer returns the options.
	childOpts := internal.ChildDialOptionsFromServer.(func(*Server) []DialOption)(srv)
	if len(childOpts) != wantChildOptsCount {
		t.Fatalf("Child dial options count from server accessor = %d, want %d", len(childOpts), wantChildOptsCount)
	}
}

// TestChildChannelOptions_Isolation tests that interceptors passed in child
// options do not execute on the parent channel or server.
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
	_ = cc.Invoke(context.Background(), "/test/method", nil, nil)
	if !parentClientInterceptorCalled {
		t.Errorf("Parent client interceptor was not called")
	}
	if childClientInterceptorCalled {
		t.Errorf("Child client interceptor was unexpectedly called on parent channel call")
	}

	// Test Server Isolation: child dial options (such as client interceptors)
	// should not be registered on the parent server.
	srv := NewServer(ChildChannelOptions(WithUnaryInterceptor(childClientInt), WithChainUnaryInterceptor(childClientInt)))
	defer srv.Stop()

	if srv.opts.unaryInt != nil || len(srv.opts.chainUnaryInts) != 0 {
		t.Errorf("Parent srv has interceptors registered from ChildChannelOptions: unaryInt=%v, chainUnaryInts=%v", srv.opts.unaryInt, srv.opts.chainUnaryInts)
	}
}

// TestChildChannelOptions_MultiLevelPropagation tests that child options
// configured on a parent channel are applied to a child channel and
// recursively propagated to grandchild channels per gRFC A110.
func (s) TestChildChannelOptions_MultiLevelPropagation(t *testing.T) {
	const customWriteBufferSize = 2048
	const wantChildOptsCount = 1

	// 1. User configures root parent channel P with O_child.
	oChild := []DialOption{WithWriteBufferSize(customWriteBufferSize)}
	parentCC, err := NewClient("passthrough:///parent",
		WithTransportCredentials(insecure.NewCredentials()),
		WithChildChannelOptions(oChild...),
	)
	if err != nil {
		t.Fatalf("NewClient for parent failed: %v", err)
	}
	defer parentCC.Close()

	// Verify parent P did NOT apply O_child to itself, but stored it for
	// children.
	if parentCC.dopts.copts.WriteBufferSize == customWriteBufferSize {
		t.Fatalf("Parent WriteBufferSize = %d, want default", customWriteBufferSize)
	}
	if len(parentCC.dopts.childDialOptions) != wantChildOptsCount {
		t.Fatalf("Parent child dial options count = %d, want %d", len(parentCC.dopts.childDialOptions), wantChildOptsCount)
	}

	// 2. Parent P creates Child channel C.
	// Per A110, C is dialed with O_child AND
	// WithChildChannelOptions(O_child...).
	parentChildOpts := parentCC.dopts.childDialOptions
	childDialOpts := append(
		[]DialOption{WithTransportCredentials(insecure.NewCredentials())},
		parentChildOpts...,
	)
	childDialOpts = append(childDialOpts, WithChildChannelOptions(parentChildOpts...))

	childCC, err := NewClient("passthrough:///child", childDialOpts...)
	if err != nil {
		t.Fatalf("NewClient for child failed: %v", err)
	}
	defer childCC.Close()

	// Verify Child C DID apply O_child to itself.
	if childCC.dopts.copts.WriteBufferSize != customWriteBufferSize {
		t.Fatalf("Child WriteBufferSize = %d, want %d", childCC.dopts.copts.WriteBufferSize, customWriteBufferSize)
	}
	// Verify Child C ALSO stored O_child in its own childDialOptions for
	// grandchildren.
	if len(childCC.dopts.childDialOptions) != wantChildOptsCount {
		t.Fatalf("Child childDialOptions count = %d, want %d", len(childCC.dopts.childDialOptions), wantChildOptsCount)
	}

	// 3. Child C creates Grandchild channel G using C's childDialOptions.
	grandchildDialOpts := append(
		[]DialOption{WithTransportCredentials(insecure.NewCredentials())},
		childCC.dopts.childDialOptions...,
	)
	grandchildDialOpts = append(grandchildDialOpts, WithChildChannelOptions(childCC.dopts.childDialOptions...))

	grandchildCC, err := NewClient("passthrough:///grandchild", grandchildDialOpts...)
	if err != nil {
		t.Fatalf("NewClient for grandchild failed: %v", err)
	}
	defer grandchildCC.Close()

	// Verify Grandchild G inherited and applied O_child.
	if grandchildCC.dopts.copts.WriteBufferSize != customWriteBufferSize {
		t.Fatalf("Grandchild WriteBufferSize = %d, want %d", grandchildCC.dopts.copts.WriteBufferSize, customWriteBufferSize)
	}
}
