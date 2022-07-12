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
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

var testHealthCheckFunc = internal.HealthCheckFunc

func newTestHealthServer() *testHealthServer {
	return newTestHealthServerWithWatchFunc(defaultWatchFunc)
}

func newTestHealthServerWithWatchFunc(f healthWatchFunc) *testHealthServer {
	return &testHealthServer{
		watchFunc: f,
		update:    make(chan struct{}, 1),
		status:    make(map[string]healthpb.HealthCheckResponse_ServingStatus),
	}
}

// defaultWatchFunc will send a HealthCheckResponse to the client whenever SetServingStatus is called.
func defaultWatchFunc(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
	if in.Service != "foo" {
		return status.Error(codes.FailedPrecondition,
			"the defaultWatchFunc only handles request with service name to be \"foo\"")
	}
	var done bool
	for {
		select {
		case <-stream.Context().Done():
			done = true
		case <-s.update:
		}
		if done {
			break
		}
		s.mu.Lock()
		resp := &healthpb.HealthCheckResponse{
			Status: s.status[in.Service],
		}
		s.mu.Unlock()
		stream.SendMsg(resp)
	}
	return nil
}

type healthWatchFunc func(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error

type testHealthServer struct {
	healthgrpc.UnimplementedHealthServer
	watchFunc healthWatchFunc
	mu        sync.Mutex
	status    map[string]healthpb.HealthCheckResponse_ServingStatus
	update    chan struct{}
}

func (s *testHealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_SERVING,
	}, nil
}

func (s *testHealthServer) Watch(in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
	return s.watchFunc(s, in, stream)
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap.
func (s *testHealthServer) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	s.mu.Lock()
	s.status[service] = status
	select {
	case <-s.update:
	default:
	}
	s.update <- struct{}{}
	s.mu.Unlock()
}

func setupHealthCheckWrapper() (hcEnterChan chan struct{}, hcExitChan chan struct{}, wrapper internal.HealthChecker) {
	hcEnterChan = make(chan struct{})
	hcExitChan = make(chan struct{})
	wrapper = func(ctx context.Context, newStream func(string) (interface{}, error), update func(connectivity.State, error), service string) error {
		close(hcEnterChan)
		defer close(hcExitChan)
		return testHealthCheckFunc(ctx, newStream, update, service)
	}
	return
}

func setupServer(t *testing.T, watchFunc healthWatchFunc) (*grpc.Server, net.Listener, *testHealthServer) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}

	var ts *testHealthServer
	if watchFunc != nil {
		ts = newTestHealthServerWithWatchFunc(watchFunc)
	} else {
		ts = newTestHealthServer()
	}
	s := grpc.NewServer()
	healthgrpc.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	t.Cleanup(func() { s.Stop() })
	return s, lis, ts
}

type clientConfig struct {
	balancerName               string
	testHealthCheckFuncWrapper internal.HealthChecker
	extraDialOption            []grpc.DialOption
}

func setupClient(t *testing.T, c *clientConfig) (*grpc.ClientConn, *manual.Resolver) {
	t.Helper()

	r := manual.NewBuilderWithScheme("whatever")
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithResolvers(r),
	}
	if c != nil {
		if c.balancerName != "" {
			opts = append(opts, grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, c.balancerName)))
		}
		if c.testHealthCheckFuncWrapper != nil {
			opts = append(opts, internal.WithHealthCheckFunc.(func(internal.HealthChecker) grpc.DialOption)(c.testHealthCheckFuncWrapper))
		}
		opts = append(opts, c.extraDialOption...)
	}

	cc, err := grpc.Dial(r.Scheme()+":///test.server", opts...)
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	t.Cleanup(func() { cc.Close() })
	return cc, r
}

func (s) TestHealthCheckWatchStateChange(t *testing.T) {
	_, lis, ts := setupServer(t, nil)

	// The table below shows the expected series of addrConn connectivity transitions when server
	// updates its health status. As there's only one addrConn corresponds with the ClientConn in this
	// test, we use ClientConn's connectivity state as the addrConn connectivity state.
	//+------------------------------+-------------------------------------------+
	//| Health Check Returned Status | Expected addrConn Connectivity Transition |
	//+------------------------------+-------------------------------------------+
	//| NOT_SERVING                  | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| SERVICE_UNKNOWN              | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| UNKNOWN                      | ->TRANSIENT FAILURE                       |
	//+------------------------------+-------------------------------------------+
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_NOT_SERVING)

	cc, r := setupClient(t, nil)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if ok := cc.WaitForStateChange(ctx, connectivity.Idle); !ok {
		t.Fatal("ClientConn is still in IDLE state when the context times out.")
	}
	if ok := cc.WaitForStateChange(ctx, connectivity.Connecting); !ok {
		t.Fatal("ClientConn is still in CONNECTING state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.TransientFailure {
		t.Fatalf("ClientConn is in %v state, want TRANSIENT FAILURE", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	if ok := cc.WaitForStateChange(ctx, connectivity.TransientFailure); !ok {
		t.Fatal("ClientConn is still in TRANSIENT FAILURE state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	if ok := cc.WaitForStateChange(ctx, connectivity.Ready); !ok {
		t.Fatal("ClientConn is still in READY state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.TransientFailure {
		t.Fatalf("ClientConn is in %v state, want TRANSIENT FAILURE", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	if ok := cc.WaitForStateChange(ctx, connectivity.TransientFailure); !ok {
		t.Fatal("ClientConn is still in TRANSIENT FAILURE state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_UNKNOWN)
	if ok := cc.WaitForStateChange(ctx, connectivity.Ready); !ok {
		t.Fatal("ClientConn is still in READY state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.TransientFailure {
		t.Fatalf("ClientConn is in %v state, want TRANSIENT FAILURE", s)
	}
}

// If Watch returns Unimplemented, then the ClientConn should go into READY state.
func (s) TestHealthCheckHealthServerNotRegistered(t *testing.T) {
	grpctest.TLogger.ExpectError("Subchannel health check is unimplemented at server side, thus health check is disabled")
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen due to err: %v", err)
	}
	go s.Serve(lis)
	defer s.Stop()

	cc, r := setupClient(t, nil)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if ok := cc.WaitForStateChange(ctx, connectivity.Idle); !ok {
		t.Fatal("ClientConn is still in IDLE state when the context times out.")
	}
	if ok := cc.WaitForStateChange(ctx, connectivity.Connecting); !ok {
		t.Fatal("ClientConn is still in CONNECTING state when the context times out.")
	}
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}
}

// In the case of a goaway received, the health check stream should be terminated and health check
// function should exit.
func (s) TestHealthCheckWithGoAway(t *testing.T) {
	s, lis, ts := setupServer(t, nil)
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// the stream rpc will persist through goaway event.
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

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}

	// server sends GoAway
	go s.GracefulStop()

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		select {
		case <-hcEnterChan:
		default:
			t.Fatal("Health check function has not entered after 5s.")
		}
		t.Fatal("Health check function has not exited after 5s.")
	}

	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
}

func (s) TestHealthCheckWithConnClose(t *testing.T) {
	s, lis, ts := setupServer(t, nil)
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}
	// server closes the connection
	s.Stop()

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		select {
		case <-hcEnterChan:
		default:
			t.Fatal("Health check function has not entered after 5s.")
		}
		t.Fatal("Health check function has not exited after 5s.")
	}
}

// addrConn drain happens when addrConn gets torn down due to its address being no longer in the
// address list returned by the resolver.
func (s) TestHealthCheckWithAddrConnDrain(t *testing.T) {
	_, lis, ts := setupServer(t, nil)
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})
	tc := testpb.NewTestServiceClient(cc)
	sc := parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)
	r.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: sc,
	})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// the stream rpc will persist through goaway event.
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

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}
	// trigger teardown of the ac
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "fake address"}}, ServiceConfig: sc})

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		select {
		case <-hcEnterChan:
		default:
			t.Fatal("Health check function has not entered after 5s.")
		}
		t.Fatal("Health check function has not exited after 5s.")
	}

	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
}

// ClientConn close will lead to its addrConns being torn down.
func (s) TestHealthCheckWithClientConnClose(t *testing.T) {
	_, lis, ts := setupServer(t, nil)
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}

	// trigger addrConn teardown
	cc.Close()

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		select {
		case <-hcEnterChan:
		default:
			t.Fatal("Health check function has not entered after 5s.")
		}
		t.Fatal("Health check function has not exited after 5s.")
	}
}

// This test is to test the logic in the createTransport after the health check function returns which
// closes the skipReset channel(since it has not been closed inside health check func) to unblock
// onGoAway/onClose goroutine.
func (s) TestHealthCheckWithoutSetConnectivityStateCalledAddrConnShutDown(t *testing.T) {
	watchFunc := func(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
		if in.Service != "delay" {
			return status.Error(codes.FailedPrecondition,
				"this special Watch function only handles request with service name to be \"delay\"")
		}
		// Do nothing to mock a delay of health check response from server side.
		// This case is to help with the test that covers the condition that setConnectivityState is not
		// called inside HealthCheckFunc before the func returns.
		select {
		case <-stream.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return nil
	}
	_, lis, ts := setupServer(t, watchFunc)
	ts.SetServingStatus("delay", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	_, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})

	// The serviceName "delay" is specially handled at server side, where response will not be sent
	// back to client immediately upon receiving the request (client should receive no response until
	// test ends).
	sc := parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "delay"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)
	r.UpdateState(resolver.State{
		Addresses:     []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: sc,
	})

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}

	select {
	case <-hcEnterChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not been invoked after 5s.")
	}
	// trigger teardown of the ac, ac in SHUTDOWN state
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: "fake address"}}, ServiceConfig: sc})

	// The health check func should exit without calling the setConnectivityState func, as server hasn't sent
	// any response.
	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not exited after 5s.")
	}
	// The deferred leakcheck will check whether there's leaked goroutine, which is an indication
	// whether we closes the skipReset channel to unblock onGoAway/onClose goroutine.
}

// This test is to test the logic in the createTransport after the health check function returns which
// closes the allowedToReset channel(since it has not been closed inside health check func) to unblock
// onGoAway/onClose goroutine.
func (s) TestHealthCheckWithoutSetConnectivityStateCalled(t *testing.T) {
	watchFunc := func(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
		if in.Service != "delay" {
			return status.Error(codes.FailedPrecondition,
				"this special Watch function only handles request with service name to be \"delay\"")
		}
		// Do nothing to mock a delay of health check response from server side.
		// This case is to help with the test that covers the condition that setConnectivityState is not
		// called inside HealthCheckFunc before the func returns.
		select {
		case <-stream.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return nil
	}
	s, lis, ts := setupServer(t, watchFunc)
	ts.SetServingStatus("delay", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan, hcExitChan, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	_, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})

	// The serviceName "delay" is specially handled at server side, where response will not be sent
	// back to client immediately upon receiving the request (client should receive no response until
	// test ends).
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "delay"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	select {
	case <-hcExitChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}

	select {
	case <-hcEnterChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not been invoked after 5s.")
	}
	// trigger transport being closed
	s.Stop()

	// The health check func should exit without calling the setConnectivityState func, as server hasn't sent
	// any response.
	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not exited after 5s.")
	}
	// The deferred leakcheck will check whether there's leaked goroutine, which is an indication
	// whether we closes the allowedToReset channel to unblock onGoAway/onClose goroutine.
}

func testHealthCheckDisableWithDialOption(t *testing.T, addr string) {
	hcEnterChan, _, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{
		testHealthCheckFuncWrapper: testHealthCheckFuncWrapper,
		extraDialOption:            []grpc.DialOption{grpc.WithDisableHealthCheck()},
	})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: addr}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-hcEnterChan:
		t.Fatal("Health check function has exited, which is not expected.")
	default:
	}
}

func testHealthCheckDisableWithBalancer(t *testing.T, addr string) {
	hcEnterChan, _, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{
		testHealthCheckFuncWrapper: testHealthCheckFuncWrapper,
	})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: addr}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "foo"
	},
	"loadBalancingConfig": [{"pick_first":{}}]
}`)})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-hcEnterChan:
		t.Fatal("Health check function has started, which is not expected.")
	default:
	}
}

func testHealthCheckDisableWithServiceConfig(t *testing.T, addr string) {
	hcEnterChan, _, testHealthCheckFuncWrapper := setupHealthCheckWrapper()
	cc, r := setupClient(t, &clientConfig{testHealthCheckFuncWrapper: testHealthCheckFuncWrapper})
	tc := testpb.NewTestServiceClient(cc)
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: addr}}})

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-hcEnterChan:
		t.Fatal("Health check function has started, which is not expected.")
	default:
	}
}

func (s) TestHealthCheckDisable(t *testing.T) {
	_, lis, ts := setupServer(t, nil)
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	// test client side disabling configuration.
	testHealthCheckDisableWithDialOption(t, lis.Addr().String())
	testHealthCheckDisableWithBalancer(t, lis.Addr().String())
	testHealthCheckDisableWithServiceConfig(t, lis.Addr().String())
}

func (s) TestHealthCheckChannelzCountingCallSuccess(t *testing.T) {
	watchFunc := func(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
		if in.Service != "channelzSuccess" {
			return status.Error(codes.FailedPrecondition,
				"this special Watch function only handles request with service name to be \"channelzSuccess\"")
		}
		return status.Error(codes.OK, "fake success")
	}
	_, lis, _ := setupServer(t, watchFunc)

	_, r := setupClient(t, nil)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "channelzSuccess"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

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

func (s) TestHealthCheckChannelzCountingCallFailure(t *testing.T) {
	watchFunc := func(s *testHealthServer, in *healthpb.HealthCheckRequest, stream healthgrpc.Health_WatchServer) error {
		if in.Service != "channelzFailure" {
			return status.Error(codes.FailedPrecondition,
				"this special Watch function only handles request with service name to be \"channelzFailure\"")
		}
		return status.Error(codes.Internal, "fake failure")
	}
	_, lis, _ := setupServer(t, watchFunc)

	_, r := setupClient(t, nil)
	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: lis.Addr().String()}},
		ServiceConfig: parseServiceConfig(t, r, `{
	"healthCheckConfig": {
		"serviceName": "channelzFailure"
	},
	"loadBalancingConfig": [{"round_robin":{}}]
}`)})

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
