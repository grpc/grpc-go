/*
 *
 * Copyright 2021 gRPC authors.
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

package server

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"

	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"

	_ "google.golang.org/grpc/xds/internal/httpfilter/router" // Register the router filter
)

/*

// Switch these to assert on Ready based off Accept() + Close()

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3routerpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	_ "google.golang.org/grpc/xds/internal/httpfilter/router" // Register the router filter
)

const (
	fakeListenerHost        = "0.0.0.0"
	fakeListenerPort        = 50051
	defaultTestTimeout      = 10 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// badListenerResource returns a listener resource for the given name which does
// not contain the `RouteSpecifier` field in the HTTPConnectionManager, and
// hence is expected to be NACKed by the client.
func badListenerResource(t *testing.T, name string) *v3listenerpb.Listener {
	hcm := testutils.MarshalAny(t, &v3httppb.HttpConnectionManager{
		HttpFilters: []*v3httppb.HttpFilter{e2e.HTTPFilter("router", &v3routerpb.Router{})},
	})
	return &v3listenerpb.Listener{
		Name:        name,
		ApiListener: &v3listenerpb.ApiListener{ApiListener: hcm},
		FilterChains: []*v3listenerpb.FilterChain{{
			Name: "filter-chain-name",
			Filters: []*v3listenerpb.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &v3listenerpb.Filter_TypedConfig{TypedConfig: hcm},
			}},
		}},
	}
}

func hostPortFromListener(t *testing.T, lis net.Listener) (string, uint32) {
	t.Helper()

	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		t.Fatalf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port)
}

// Creates a local TCP net.Listener and creates a listenerWrapper by passing
// that and the provided xDS client.
//
// Returns the following:
//   - the ready channel of the listenerWrapper
//   - host of the listener
//   - port of the listener
//   - listener resource name to use when requesting this resource from the
//     management server
func createListenerWrapper(t *testing.T, xdsC XDSClient) (<-chan struct{}, string, uint32, string) {
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local TCP listener: %v", err)
	}

	host, port := hostPortFromListener(t, lis)
	lisResourceName := fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port))))
	params := ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: lisResourceName,
		XDSClient:            xdsC,
	}
	l, readyCh := NewListenerWrapper(params)
	if l == nil {
		t.Fatalf("NewListenerWrapper(%+v) returned nil", params)
	}
	t.Cleanup(func() { l.Close() })
	return readyCh, host, port, lisResourceName
}

// Tests the case where a listenerWrapper is created and following happens:
//
//   - the management server returns a Listener resource that is NACKed. Test
//     verifies that the listenerWrapper does not become ready.
//   - the management server returns a Listener resource that does not match the
//     address to which our net.Listener is bound to. Test verifies that the
//     listenerWrapper does not become ready.
//   - the management server returns a Listener resource that that matches the
//     address to which our net.Listener is bound to. Also, it contains an
//     inline Route Configuration. Test verifies that the listenerWrapper
//     becomes ready.

// write same test, but ready downstream check is if Conns accept + close
// or if it actually tries to match filter chain logic

func (s) TestListenerWrapper_InlineRouteConfig(t *testing.T) {
	mgmtServer, nodeID, ldsResourceNamesCh, _, xdsC := xdsSetupFoTests(t)
	readyCh, host, port, lisResourceName := createListenerWrapper(t, xdsC)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, ldsResourceNamesCh, []string{lisResourceName})

	// Configure the management server with a listener resource that is expected
	// to be NACKed.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{badListenerResource(t, lisResourceName)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that there is no message on the ready channel.
	sCtx, sCtxCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-readyCh:
		t.Fatalf("Ready channel written to after receipt of a bad Listener update")
	case <-sCtx.Done():
	}

	// Configure the management server with a listener resource that does not
	// match the address to which our listener is bound to.
	resources.Listeners = []*v3listenerpb.Listener{e2e.DefaultServerListener(host, port+1, e2e.SecurityLevelNone, route1)}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that there is no message on the ready channel.
	select {
	case <-readyCh:
		t.Fatalf("Ready channel written to after receipt of a bad Listener update")
	case <-sCtx.Done():
	}

	// Configure the management server with a Listener resource that contains
	// the expected host and port. Also, it does not contain any rds names that
	// need reolution. Therefore the listenerWrapper is expected to become
	// ready.
	resources.Listeners = []*v3listenerpb.Listener{e2e.DefaultServerListener(host, port, e2e.SecurityLevelNone, route1)}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the listener wrapper becomes ready.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for the ready channel to be written to after receipt of a good Listener update")
	case <-readyCh:
	}
}

// Tests the case where a listenerWrapper is created and the management server
// returns a Listener resource that specifies the name of a Route Configuration
// resource. The test verifies that the listenerWrapper does not become ready
// when waiting for the Route Configuration resource and becomes ready once it
// receives the Route Configuration resource.

// write same test, but ready downstream check is if Conns accept + close
// or if it actually tries to match filter chain logic

func (s) TestListenerWrapper_RouteNames(t *testing.T) {
	mgmtServer, nodeID, ldsResourceNamesCh, rdsResourceNamesCh, xdsC := xdsSetupFoTests(t)
	readyCh, host, port, lisResourceName := createListenerWrapper(t, xdsC)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, ldsResourceNamesCh, []string{lisResourceName})

	// Configure the management server with a listener resource that specifies
	// the name of RDS resources that need to be resolved.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, route1)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	waitForResourceNames(ctx, t, rdsResourceNamesCh, []string{route1})

	// Verify that there is no message on the ready channel.
	sCtx, sCtxCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCtxCancel()
	select {
	case <-readyCh:
		t.Fatalf("Ready channel written to without rds configuration specified")
	case <-sCtx.Done():
	}

	// Configure the management server with the route configuration resource
	// specified by the listener resource.
	resources.Routes = []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(route1, lisResourceName, clusterName)}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// All of the xDS updates have completed, so can expect to send a ping on
	// good update channel.
	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for the ready channel to be written to after receipt of a good rds update")
	case <-readyCh:
	}
}

type tempError struct{}

func (tempError) Error() string {
	return "listenerWrapper test temporary error"
}

func (tempError) Temporary() bool {
	return true
}

// connAndErr wraps a net.Conn and an error.
type connAndErr struct {
	conn net.Conn
	err  error
}

// fakeListener allows the user to inject conns returned by Accept().
type fakeListener struct {
	acceptCh chan connAndErr
	closeCh  *testutils.Channel
}

func (fl *fakeListener) Accept() (net.Conn, error) {
	cne, ok := <-fl.acceptCh
	if !ok {
		return nil, errors.New("a non-temporary error")
	}
	return cne.conn, cne.err
}

func (fl *fakeListener) Close() error {
	fl.closeCh.Send(nil)
	return nil
}

func (fl *fakeListener) Addr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: fakeListenerPort,
	}
}

// fakeConn overrides LocalAddr, RemoteAddr and Close methods.
type fakeConn struct {
	net.Conn
	local, remote net.Addr
	closeCh       *testutils.Channel
}

func (fc *fakeConn) LocalAddr() net.Addr {
	return fc.local
}

func (fc *fakeConn) RemoteAddr() net.Addr {
	return fc.remote
}

func (fc *fakeConn) Close() error {
	fc.closeCh.Send(nil)
	return nil
}

// Tests the case where a listenerWrapper is created with a fake net.Listener.
// The test verifies that the listenerWrapper becomes ready once it receives
// configuration from the management server. The test then performs the
// following:
//   - injects a non-temp error via the fake net.Listener and verifies that
//     Accept() returns with the same error.
//   - injects a temp error via the fake net.Listener and verifies that Accept()
//     backs off.
//   - injects a fake net.Conn via the fake net.Listener. This Conn is expected
//     to match the filter chains on the Listener resource. Verifies that
//     Accept() does not return an error in this case.

// Same test, but poll instead of using Accept channel to switch it to ready

func (s) TestListenerWrapper_Accept(t *testing.T) {
	boCh := testutils.NewChannel()
	origBackoffFunc := backoffFunc
	backoffFunc = func(v int) time.Duration {
		boCh.Send(v)
		return 0
	}
	defer func() { backoffFunc = origBackoffFunc }()

	mgmtServer, nodeID, ldsResourceNamesCh, _, xdsC := xdsSetupFoTests(t)

	// Create a listener wrapper with a fake listener and verify that it
	// extracts the host and port from the passed in listener.
	lis := &fakeListener{
		acceptCh: make(chan connAndErr, 1),
		closeCh:  testutils.NewChannel(),
	}
	lisResourceName := fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(fakeListenerHost, strconv.Itoa(int(fakeListenerPort))))
	params := ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: lisResourceName,
		XDSClient:            xdsC,
	}
	l, readyCh := NewListenerWrapper(params) // Server now immediately serves, rather than this going ready on first update
	if l == nil {
		t.Fatalf("NewListenerWrapper(%+v) returned nil", params)
	}
	lw, ok := l.(*listenerWrapper)
	if !ok {
		t.Fatalf("NewListenerWrapper(%+v) returned listener of type %T want *listenerWrapper", params, l)
	}
	if lw.addr != fakeListenerHost || lw.port != strconv.Itoa(fakeListenerPort) {
		t.Fatalf("listenerWrapper has host:port %s:%s, want %s:%d", lw.addr, lw.port, fakeListenerHost, fakeListenerPort)
	}
	defer l.Close()

	// Verify that the expected listener resource is requested.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	waitForResourceNames(ctx, t, ldsResourceNamesCh, []string{lisResourceName})

	// Configure the management server with a listener resource.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListener(fakeListenerHost, fakeListenerPort, e2e.SecurityLevelNone, route1)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Verify that the listener wrapper becomes ready.
	select {
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for the ready channel to be written to after receipt of a good Listener update")
	case <-readyCh: // I feel like should eventually report serving, handleRDSUpdates does it sync so whenever that function gets invoked is what you block on here...
	}

	// Push a non-temporary error into Accept().
	nonTempErr := errors.New("a non-temporary error")
	lis.acceptCh <- connAndErr{err: nonTempErr}
	if _, err := lw.Accept(); err != nonTempErr {
		t.Fatalf("listenerWrapper.Accept() returned error: %v, want: %v", err, nonTempErr)
	}

	// (does this behavior stay the same?)
	// Invoke Accept() in a goroutine since we expect it to swallow:
	// 1. temporary errors returned from the underlying listener
	// 2. errors related to finding a matching filter chain for the incoming
	// 	  connection.
	errCh := testutils.NewChannel()
	go func() {
		conn, err := lw.Accept()
		if err != nil {
			errCh.Send(err)
			return
		}
		if _, ok := conn.(*connWrapper); !ok {
			errCh.Send(errors.New("listenerWrapper.Accept() returned a Conn of type %T, want *connWrapper"))
			return
		}
		errCh.Send(nil)
	}()

	// Push a temporary error into Accept() and verify that it backs off.
	lis.acceptCh <- connAndErr{err: tempError{}}
	if _, err := boCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Accept() to backoff on temporary errors: %v", err)
	}

	// Push a fakeConn which matches the filter chains configured on the
	// received Listener resource. Verify that Accept() returns.
	fc := &fakeConn{ // here you go, inject certain conn properties, I don't know if you can inject this e2e though...
		local:   &net.TCPAddr{IP: net.IPv4(192, 168, 1, 2)},
		remote:  &net.TCPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 80},
		closeCh: testutils.NewChannel(),
	}
	lis.acceptCh <- connAndErr{conn: fc}
	if _, err := errCh.Receive(ctx); err != nil {
		t.Fatalf("error when waiting for Accept() to return the conn on filter chain match: %v", err)
	}
}

func (s) TestLisWrapper(t *testing.T) {

	// I don't need this anymore I think
	boCh := testutils.NewChannel()
	origBackoffFunc := backoffFunc
	backoffFunc = func(v int) time.Duration {
		boCh.Send(v)
		return 0
	}
	defer func() { backoffFunc = origBackoffFunc }()
	// I don't think I need this codeblock anymore either



	mgmtServer, nodeID, ldsResourceNamesCh, _, xdsC := xdsSetupFoTests(t)



	// Create a listener wrapper with a fake listener and verify that it
	// extracts the host and port from the passed in listener.
	lis := &fakeListener{ // lis wrapper wraps a listener, also need to wrap it for test
		acceptCh: make(chan connAndErr, 1),
		closeCh:  testutils.NewChannel(),
	}
	lisResourceName := fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(fakeListenerHost, strconv.Itoa(int(fakeListenerPort))))
	params := ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: lisResourceName,
		XDSClient:            xdsC,
	}

	lis := NewListenerWrapper(params) // these params have to get to the same thing set up client etc.

	// pass it to a server to serve

	// before it gets full rds should Accept() and Close() connections


	// lis wrapper wraps a lis, need to figure out how wrapped lis fits in

	// configure LDS pointing to RDS only

	// before RDS comes in
	// Accept + Close() (needs a server, so perhaps e2e) ^^^ creation of components to unit test

	// previously this was testing writes to update channel from good update,
	// this is wrong and I no longer need to test this

}



// What exactly is above testing, and do I want to keep it or change it

// this component has changed a lot too

// specific unit test case scenarios listed in e2e tests...:

// Before wrote ready when first lds
// Serve serves on the lis, before used to block on the lis state
// now Serves() but not ready so Accept() + Close()...invariant to test

// used to block Serve() on it leaving READY, now just Serves and Accepts() and Closes()
// I don't know the layering to change here

// and has current and pending (but maybe best to test this e2e)

*/

// essentially before tested it going ready signal, now it doesn't block

// could still test internal state...i.e. serving and non serving
// Also test other properties? What other properties happen?

// test mode shifts? Can't really test conn accepts
// since conn accepts are tested e2e

// listener wrapper tests are weird

// now starts ready and immediately accepts.
// Tested in e2e but how else?

// what are invaraints to test here?

// I think only thing to test is
// mode callback - starts in mode non serving

// lds + rds (never transitions into serving)
// gets the rds (transitions into serving)

type verifyMode struct {
	modeCh chan connectivity.ServingMode
}

func (vm *verifyMode) verifyModeCallback(_ net.Addr, mode connectivity.ServingMode, _ error) {
	// send on a channel of 1...not set initially?
	vm.modeCh <- mode
}

func hostPortFromListener(t *testing.T, lis net.Listener) (string, uint32) {
	t.Helper()

	host, p, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		t.Fatalf("net.SplitHostPort(%s) failed: %v", lis.Addr().String(), err)
	}
	port, err := strconv.ParseInt(p, 10, 32)
	if err != nil {
		t.Fatalf("strconv.ParseInt(%s, 10, 32) failed: %v", p, err)
	}
	return host, uint32(port)
}

// test as a result of fake listener? plumb that in or do something else?
// Creates a local TCP net.Listener and creates a listenerWrapper by passing
// that and the provided xDS client.
//
// Returns the following:
//   - the ready channel of the listenerWrapper
//   - host of the listener
//   - port of the listener
//   - listener resource name to use when requesting this resource from the
//     management server
/*func createListenerWrapper(t *testing.T, xdsC XDSClient) (<-chan struct{}, string, uint32, string) {
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local TCP listener: %v", err)
	}

	host, port := hostPortFromListener(t, lis)
	lisResourceName := fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port))))
	params := ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: lisResourceName,
		XDSClient:            xdsC,
		ModeCallback:         verifyModeCallback, // somehow will need to get a channel/object ref here, rather than pass it through stack local var
	}
	l := NewListenerWrapper(params)
	if l == nil {
		t.Fatalf("NewListenerWrapper(%+v) returned nil", params)
	}
	t.Cleanup(func() { l.Close() })
	// verify channel for mode changes?
	return ch, host, port, lisResourceName
}*/

func (s) TestListenerWrapper(t *testing.T) {
	mgmtServer, nodeID, ldsResourceNamesCh, rdsResourceNamesCh, xdsC := xdsSetupFoTests(t)
	// readyCh, host, port, lisResourceName := createListenerWrapper(t, xdsC)
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local TCP listener: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	modeCh := make(chan connectivity.ServingMode, 1)
	vm := verifyMode{
		modeCh: modeCh,
	}
	host, port := hostPortFromListener(t, lis)
	lisResourceName := fmt.Sprintf(e2e.ServerListenerResourceNameTemplate, net.JoinHostPort(host, strconv.Itoa(int(port))))
	params := ListenerWrapperParams{
		Listener:             lis,
		ListenerResourceName: lisResourceName,
		XDSClient:            xdsC,
		ModeCallback:         vm.verifyModeCallback, // somehow will need to get a channel/object ref here, rather than pass it through stack local var
	}
	l := NewListenerWrapper(params)
	if l == nil {
		t.Fatalf("NewListenerWrapper(%+v) returned nil", params)
	}
	defer l.Close()
	waitForResourceNames(ctx, t, ldsResourceNamesCh, []string{lisResourceName})
	/*lw := NewListenerWrapper(ListenerWrapperParams{
		ModeCallback: func(addr net.Addr, mode connectivity.ServingMode, err error),
	}/*any params here to test?)*/
	// this thing is a net listener...how did test get serving mode changes

	// LDS pointing to RDS - not ready (verify no mode update sent)? (verify LDS request gets sent)

	// invoke resource not found - should go ready (optional)


	// update rds - should go ready
	// Configure the management server with a listener resource that specifies
	// the name of RDS resources that need to be resolved.
	resources := e2e.UpdateOptions{
		NodeID:         nodeID,
		Listeners:      []*v3listenerpb.Listener{e2e.DefaultServerListenerWithRouteConfigName(host, port, e2e.SecurityLevelNone, route1)},
		SkipValidation: true,
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	waitForResourceNames(ctx, t, rdsResourceNamesCh, []string{route1})

	// Verify that there is no message on the ready channel. (no mode change argument)
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()

	select {
	case mode := <-modeCh:
		t.Fatalf("received mode change to %v when no mode expected", mode)
	case <-sCtx.Done():
	}

	// Configure the management server with the route configuration resource
	// specified by the listener resource.
	resources.Routes = []*v3routepb.RouteConfiguration{e2e.DefaultRouteConfig(route1, lisResourceName, clusterName)}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// mode goes ready
	select {
	case <-ctx.Done():
		t.Fatalf("timeout waiting for mode change")
	case mode := <-modeCh:
		if mode != connectivity.ServingModeServing {
			t.Fatalf("mode change received: %v, want: %v", mode, connectivity.ServingModeServing)
		}
	}

	// invoke lds resource not found - should go unready

}
