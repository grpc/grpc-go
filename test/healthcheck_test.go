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

package test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	_ "google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

const (
	sc = `{
		"healthCheckConfig": {
			"serviceName": "grpc.health.v1.Health"
		}
	}`
	defaultTimeout = 10 * time.Second
)

var (
	testHealthCheckFunc  = internal.HealthCheckFunc
	defaultServiceConfig = parseCfg(sc)
)

// testHealthServer is a fake implementation of the HealthService with a
// customizable Watch method. It does not support the Check method, and will
// panic if called.
type testHealthServer struct {
	// Guarantees we satisfy this interface; panics if unimplemented methods are
	// called.
	healthpb.HealthServer
	// Customizable implementation of the Watch method.
	watchFunc func(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error
}

func (s *testHealthServer) Watch(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
	return s.watchFunc(in, stream)
}

// setupHealthCheckWrapper creates a wrapper function of type
// internal.HealthChecker that simply wraps the default client-side health
// check function (exported by google.golang.org/grpc/health). Additionally, it
// returns a couple of channels to indicate when the health check function was
// entered and when it exited.
func setupHealthCheckWrapper() (chan struct{}, chan struct{}, internal.HealthChecker) {
	enterCh := make(chan struct{})
	exitCh := make(chan struct{})
	return enterCh, exitCh, func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(enterCh)
		defer close(exitCh)
		return testHealthCheckFunc(ctx, newStream, update, service)
	}
}

// Configurable options for test setup.
type opts struct {
	// If set, starts the default implementation of the health service.
	enableHealthServer bool
	// In most cases, one should use the 'enableHealthServer' option to start the
	// default health service implementation. Only when a test requires a
	// non-default health service implementation to fake specific behavior, it
	// should be passed here.
	healthServer healthpb.HealthServer
	// If set, overrides the default healthCheck function on the client side
	// using the internal.WithHealthCheckFunc dialOption.
	healthFunc internal.HealthChecker
	// Dial options used when creating the ClientConn.
	dialOpts []grpc.DialOption
	// If set, overrides the ServiceConfig returned by the manual resolver.
	overrideSC serviceconfig.Config
}

// setup starts a grpc.Server and optionally a health.Server. It also dials the
// grpc.Server to create a ClientConn. It uses a manual resolver onto which it
// pushes the address of the started grpc.Server.
func setup(t *testing.T, o opts) (*test, *manual.Resolver, func(), *grpc.ClientConn) {
	t.Helper()

	e := tcpClearRREnv
	te := newTest(t, e)
	if o.healthServer != nil {
		te.healthServer = o.healthServer
	}
	ts := &testServer{security: e.security}
	if o.enableHealthServer {
		ts.enableHealthServer = true
	}
	te.startServer(ts)

	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	te.resolverScheme = r.Scheme()
	// We require non-blocking dials in all tests here because we use a manual
	// resolver and push updates on it.
	te.nonBlockingDial = true

	opts := o.dialOpts
	if o.healthFunc != nil {
		opts = append(opts, internal.WithHealthCheckFunc.(func(internal.HealthChecker) grpc.DialOption)(o.healthFunc))
	}
	te.customDialOptions = opts
	cc := te.clientConn()

	sc := defaultServiceConfig
	if o.overrideSC != nil {
		sc = o.overrideSC
	}
	r.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: te.srvAddr}},
		ServiceConfig: sc,
	})

	return te, r, rcleanup, cc
}

// waitForStateChange waits for the ClientConn to move out of 'fromState'.
func waitForStateChange(ctx context.Context, t *testing.T, cc *grpc.ClientConn, fromState connectivity.State) {
	t.Helper()
	if !cc.WaitForStateChange(ctx, fromState) {
		t.Fatalf("clientConn is still in %v state when the context times out", fromState)
	}
}

// checkConnState verifies the current state of the ClientConn.
func checkConnState(t *testing.T, cc *grpc.ClientConn, wantState connectivity.State) {
	t.Helper()
	if s := cc.GetState(); s != wantState {
		t.Fatalf("clientConn is in %v state, want %v", s, wantState)
	}
}

// TestHealthCheckWatchStateChange updates the health status of the grpc.Server
// (by calling SetServingStatus on the associated health.Server) and verifies
// that the ClientConn goes through expected connectivity state changes.
func (s) TestHealthCheckWatchStateChange(t *testing.T) {
	te, _, rcleanup, cc := setup(t, opts{enableHealthServer: true})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_NOT_SERVING)

	// The table below shows the expected series of addrConn connectivity
	// transitions when server updates its health status. As there's only one
	// addrConn corresponds with the ClientConn in this test, we use ClientConn's
	// connectivity state as the addrConn connectivity state.
	//+------------------------------+-------------------------------------------+
	//| Health Check Returned Status | Expected addrConn Connectivity Transition |
	//+------------------------------+-------------------------------------------+
	//| NOT_SERVING                  | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| SERVICE_UNKNOWN              | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| UNKNOWN                      | ->TRANSIENT FAILURE                       |
	//+------------------------------+-------------------------------------------+

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	for _, op := range []func(){
		func() { waitForStateChange(ctx, t, cc, connectivity.Idle) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Connecting) },
		func() { checkConnState(t, cc, connectivity.TransientFailure) },
		func() { te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING) },
		func() { waitForStateChange(ctx, t, cc, connectivity.TransientFailure) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Connecting) },
		func() { checkConnState(t, cc, connectivity.Ready) },
		func() { te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVICE_UNKNOWN) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Ready) },
		func() { checkConnState(t, cc, connectivity.TransientFailure) },
		func() { te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING) },
		func() { waitForStateChange(ctx, t, cc, connectivity.TransientFailure) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Connecting) },
		func() { checkConnState(t, cc, connectivity.Ready) },
		func() { te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_UNKNOWN) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Ready) },
		func() { checkConnState(t, cc, connectivity.TransientFailure) },
	} {
		op()
	}
}

// TestHealthCheckHealthServerNotRegistered starts a grpc.Server without a
// health.Server and verifies that the the ClientConn goes into READY state.
func (s) TestHealthCheckHealthServerNotRegistered(t *testing.T) {
	te, _, rcleanup, cc := setup(t, opts{})
	defer te.tearDown()
	defer rcleanup()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	for _, op := range []func(){
		func() { waitForStateChange(ctx, t, cc, connectivity.Idle) },
		func() { waitForStateChange(ctx, t, cc, connectivity.Connecting) },
		func() { checkConnState(t, cc, connectivity.Ready) },
	} {
		op()
	}
}

// makeStreamAndRPC makes a streaming RPC call to the testServer and returns
// the associated stream, which is generally used to send/receive more
// streaming messages.
func makeStreamAndRPC(ctx context.Context, t *testing.T, cc *grpc.ClientConn) testpb.TestService_FullDuplexCallClient {
	t.Helper()
	tc := testpb.NewTestServiceClient(cc)
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam := []*testpb.ResponseParameters{{Size: 1}}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(1))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseParameters: respParam,
		Payload:            payload,
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
	return stream
}

// makeStreamingRPC uses the provided steam to send and receive a streaming
// message.
func makeStreamingRPC(t *testing.T, stream testpb.TestService_FullDuplexCallClient) {
	t.Helper()
	respParam := []*testpb.ResponseParameters{{Size: 1}}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(1))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseParameters: respParam,
		Payload:            payload,
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
}

// TestHealthCheckWithGoAway starts a grpc.Server in good health, and starts a
// streaming RPC call. It then calls GracefulStop on the server, It expects the
// health check stream to terminate and the health check function to exit.
// Meanwhile the client stream should still be able to send/receive messages.
func (s) TestHealthCheckWithGoAway(t *testing.T) {
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{enableHealthServer: true, healthFunc: hfWrapper})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// make some rpcs to make sure connection is working.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// the stream rpc will persist through goaway event.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	stream := makeStreamAndRPC(ctx, t, cc)

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	// server sends GoAway
	go te.srv.GracefulStop()

	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		select {
		case <-enterCh:
		default:
			t.Fatalf("health check function has not entered after %v", defaultTimeout)
		}
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
	makeStreamingRPC(t, stream)
	stream.CloseSend()
	verifyHealthCheckErrCode(t, 1*time.Second, cc, defaultHealthService, codes.Unavailable)
}

// TestHealthCheckWithServerClose starts a grpc.Server in good health, makes a
// few unary RPCs and calls Close() on the server.  It expects the health check
// stream to terminate and the health check function to exit.
func (s) TestHealthCheckWithServerClose(t *testing.T) {
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{enableHealthServer: true, healthFunc: hfWrapper})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// make some rpcs to make sure connection is working.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	// server closes the connection
	te.srv.Stop()

	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		select {
		case <-enterCh:
		default:
			t.Fatalf("health check function has not entered after %v", defaultTimeout)
		}
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
	verifyHealthCheckErrCode(t, 1*time.Second, cc, defaultHealthService, codes.Unavailable)
}

// TestHealthCheckWithAddrConnDrain tests the scenario where an addrConn drain
// happens when addrConn gets torn down due to its address being no longer in
// the address list returned by the resolver. While a stream created before the
// resolver event continues to work, it expects the health check stream to
// terminate and the health check function to exit.
func (s) TestHealthCheckWithAddrConnDrain(t *testing.T) {
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, r, rcleanup, cc := setup(t, opts{enableHealthServer: true, healthFunc: hfWrapper})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// Make a streaming rpc to the exported test service. This stream should
	// persist after the resolver event..
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	stream := makeStreamAndRPC(ctx, t, cc)

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	// trigger teardown of the addrConn.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{}, ServiceConfig: defaultServiceConfig})

	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		select {
		case <-enterCh:
		default:
			t.Fatalf("health check function has not entered after %v", defaultTimeout)
		}
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
	makeStreamingRPC(t, stream)
	stream.CloseSend()
	verifyHealthCheckErrCode(t, 1*time.Second, cc, defaultHealthService, codes.Unavailable)
}

// TestHealthCheckWithClientConnClose verifies that a ClientConn close will
// lead to its addrConns being torn down and the health check stream to
// terminate and the health check function to exit.
func (s) TestHealthCheckWithClientConnClose(t *testing.T) {
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{enableHealthServer: true, healthFunc: hfWrapper})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// make some rpcs to make sure connection is working.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	// trigger addrConn teardown
	cc.Close()

	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		select {
		case <-enterCh:
		default:
			t.Fatalf("health check function has not entered after %v", defaultTimeout)
		}
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
}

// TestHealthCheckWithoutReportHealthCalledAddrConnShutDown creates a fake
// health service implementation whose Watch() function takes an inordinate
// amount of time to return. In the meantime, the addrConn is shutdown and the
// test verifies that the healthCheck report function is not invoked, and no
// resources from the ClientConn or addrConn are leaked.
func (s) TestHealthCheckWithoutReportHealthCalledAddrConnShutDown(t *testing.T) {
	delayedHealthServer := &testHealthServer{
		watchFunc: func(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
			select {
			case <-stream.Context().Done():
			case <-time.After(2 * defaultTimeout):
			}
			return nil
		},
	}
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, r, rcleanup, _ := setup(t, opts{
		healthServer: delayedHealthServer,
		healthFunc:   hfWrapper,
	})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	select {
	case <-enterCh:
	case <-time.After(defaultTimeout):
		t.Fatalf("health check function has not been invoked after %v", defaultTimeout)
	}

	// trigger teardown of the addrConn.
	r.UpdateState(resolver.State{Addresses: []resolver.Address{}, ServiceConfig: defaultServiceConfig})

	// The health check func should exit without calling the reportHealth func,
	// as server hasn't sent any response.
	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
}

// TestHealthCheckWithoutReportHealthCalled creates a fake health service
// implementation whose Watch() function takes an inordinate amount of time to
// return. In the meantime, the transport is closed (server is shutdown) and
// the test verifies that the healthCheck report function is not invoked, and
// no resources from the ClientConn or addrConn are leaked.
func (s) TestHealthCheckWithoutReportHealthCalled(t *testing.T) {
	delayedHealthServer := &testHealthServer{
		watchFunc: func(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
			select {
			case <-stream.Context().Done():
			case <-time.After(2 * defaultTimeout):
			}
			return nil
		},
	}
	enterCh, exitCh, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, _ := setup(t, opts{
		healthServer: delayedHealthServer,
		healthFunc:   hfWrapper,
	})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	select {
	case <-exitCh:
		t.Fatal("health check function has exited, which is not expected.")
	default:
	}

	select {
	case <-enterCh:
	case <-time.After(defaultTimeout):
		t.Fatalf("health check function has not been invoked after %v", defaultTimeout)
	}

	// trigger transport being closed
	te.srv.Stop()

	// The health check func should exit without calling the reportHealth func,
	// as server hasn't sent any response.
	select {
	case <-exitCh:
	case <-time.After(defaultTimeout):
		t.Fatalf("health check function has not exited after %v", defaultTimeout)
	}
}

// TestHealthCheckDisableWithDisableHealthCheckDialOption verifies that
// client-side health checking is disabled using the
// grpc.WithDisableHealthCheck dialOption.
func (s) TestHealthCheckDisableWithDisableHealthCheckDialOption(t *testing.T) {
	enterCh, _, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{
		enableHealthServer: true,
		healthFunc:         hfWrapper,
		dialOpts:           []grpc.DialOption{grpc.WithDisableHealthCheck()},
	})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// send some rpcs to make sure transport has been created and is ready for use.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-enterCh:
		t.Fatal("health check function has started, which is not expected.")
	default:
	}
}

// TestHealthCheckDisableWithBalancerNameDialOption verifies that client-side
// health checking is disabled using a balancer which does not support it.
func (s) TestHealthCheckDisableWithBalancerNameDialOption(t *testing.T) {
	enterCh, _, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{
		enableHealthServer: true,
		healthFunc:         hfWrapper,
		dialOpts:           []grpc.DialOption{grpc.WithBalancerName("pick_first")},
	})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// send some rpcs to make sure transport has been created and is ready for use.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-enterCh:
		t.Fatal("health check function has started, which is not expected.")
	default:
	}
}

// TestHealthCheckDisableWithServiceConfig verifies that client-side health
// checking is disabled using a ServiceConfig that disables it.
func (s) TestHealthCheckDisableWithServiceConfig(t *testing.T) {
	enterCh, _, hfWrapper := setupHealthCheckWrapper()
	te, _, rcleanup, cc := setup(t, opts{
		enableHealthServer: true,
		healthFunc:         hfWrapper,
		// Update the serviceConfig to have no healthCheckConfig.
		overrideSC: parseCfg(`{"loadBalancingPolicy": "round_robin"}`),
	})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	// send some rpcs to make sure transport has been created and is ready for use.
	tc := testpb.NewTestServiceClient(cc)
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-enterCh:
		t.Fatal("health check function has started, which is not expected.")
	default:
	}
}

// TestHealthCheckChannelzCountingCallSuccess uses channelz metrics to verify
// that the healthCheck calls succeeded.
func (s) TestHealthCheckChannelzCountingCallSuccess(t *testing.T) {
	te, _, rcleanup, cc := setup(t, opts{enableHealthServer: true})
	defer te.tearDown()
	defer rcleanup()
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)

	if err := verifyResultWithDelay(func() (bool, error) {
		cm, _ := channelz.GetTopChannels(0, 0)
		if len(cm) == 0 {
			return false, errors.New("channelz.GetTopChannels return 0 top channel")
		}
		if len(cm[0].SubChans) == 0 {
			return false, errors.New("there is 0 subchannel")
		}
		var id int64
		for k := range cm[0].SubChans {
			id = k
			break
		}
		scm := channelz.GetSubChannel(id)
		if scm == nil || scm.ChannelData == nil {
			return false, errors.New("nil subchannel metric or nil subchannel metric ChannelData returned")
		}
		// exponential backoff retry may result in more than one health check call.
		if scm.ChannelData.CallsStarted > 0 && scm.ChannelData.CallsSucceeded > 0 && scm.ChannelData.CallsFailed == 0 {
			return true, nil
		}
		return false, fmt.Errorf("got %d CallsStarted, %d CallsSucceeded, want >0 >0", scm.ChannelData.CallsStarted, scm.ChannelData.CallsSucceeded)
	}); err != nil {
		t.Fatal(err)
	}
}

// TestHealthCheckChannelzCountingCallSuccess uses a fake health service
// implementation which always returns an error and uses channelz metrics to
// verify that the healthCheck calls failed.
func (s) TestHealthCheckChannelzCountingCallFailure(t *testing.T) {
	errHealthServer := &testHealthServer{
		watchFunc: func(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
			return status.Error(codes.Internal, "fake failure")
		},
	}
	te, _, rcleanup, _ := setup(t, opts{healthServer: errHealthServer})
	defer te.tearDown()
	defer rcleanup()

	if err := verifyResultWithDelay(func() (bool, error) {
		cm, _ := channelz.GetTopChannels(0, 0)
		if len(cm) == 0 {
			return false, errors.New("channelz.GetTopChannels return 0 top channel")
		}
		if len(cm[0].SubChans) == 0 {
			return false, errors.New("there is 0 subchannel")
		}
		var id int64
		for k := range cm[0].SubChans {
			id = k
			break
		}
		scm := channelz.GetSubChannel(id)
		if scm == nil || scm.ChannelData == nil {
			return false, errors.New("nil subchannel metric or nil subchannel metric ChannelData returned")
		}
		// exponential backoff retry may result in more than one health check call.
		if scm.ChannelData.CallsStarted > 0 && scm.ChannelData.CallsFailed > 0 && scm.ChannelData.CallsSucceeded == 0 {
			return true, nil
		}
		return false, fmt.Errorf("got %d CallsStarted, %d CallsFailed, want >0, >0", scm.ChannelData.CallsStarted, scm.ChannelData.CallsFailed)
	}); err != nil {
		t.Fatal(err)
	}
}
