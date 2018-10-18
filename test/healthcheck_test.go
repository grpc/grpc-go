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
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	_ "google.golang.org/grpc/healthcheck"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

var testHealthCheckFunc = internal.HealthCheckFunc

func replaceHealthCheckFunc(f func(context.Context, func() (interface{}, error), func(bool), string) error) func() {
	oldHcFunc := internal.HealthCheckFunc
	internal.HealthCheckFunc = f
	return func() {
		internal.HealthCheckFunc = oldHcFunc
	}
}

func newTestHealthServer() *testHealthServer {
	return &testHealthServer{
		update: make(chan struct{}, 1),
		status: make(map[string]healthpb.HealthCheckResponse_ServingStatus),
	}
}

type testHealthServer struct {
	mu     sync.Mutex
	status map[string]healthpb.HealthCheckResponse_ServingStatus
	update chan struct{}
}

func (s *testHealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_SERVING,
	}, nil
}

func (s *testHealthServer) Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	// service string is used here to switch between the behaviors of Watch.
	switch in.Service {
	case "foo":
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
	case "delay":
		// Do nothing to mock a delay of health check response from server side.
		// This case is to help with the test that covers the condition that reportHealth is not
		// called inside HealthCheckFunc before the func returns.
		select {
		case <-stream.Context().Done():
		case <-time.After(5 * time.Second):
		}
		return nil
	default:
		return nil
	}
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

func TestHealthCheckWatchStateChange(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	go s.Serve(lis)
	defer s.Stop()

	// The table below shows the expected series of addrConn connectivity transitions when server
	// updates its health status. As there's only one addrConn corresponds with the ClientConn in this
	// test, we use ClientConn's connectivity state as the addrConn connectivity state.
	//+------------------------------+-------------------------------------------+
	//| Health Check Returned Status | Expected addrConn Connectivity Transition |
	//		+------------------------------+-------------------------------------------+
	//| NOT_SERVING                  | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| SERVICE_UNKNOWN              | ->TRANSIENT FAILURE                       |
	//| SERVING                      | ->READY                                   |
	//| UNKNOWN                      | ->TRANSIENT FAILURE                       |
	//		+------------------------------+-------------------------------------------+
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_NOT_SERVING)
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if ok := cc.WaitForStateChange(ctx, connectivity.Idle); !ok {
		t.Fatal("ClientConn is still in IDLE state after 5s.")
	}
	if ok := cc.WaitForStateChange(ctx, connectivity.Connecting); !ok {
		t.Fatal("ClientConn is still in CONNECTING state after 5s.")
	}
	cancel()
	if s := cc.GetState(); s != connectivity.TransientFailure {
		t.Fatalf("ClientConn is in %v state, want TRANSIENT FAILURE", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if ok := cc.WaitForStateChange(ctx, connectivity.TransientFailure); !ok {
		t.Fatal("ClientConn is still in TRANSIENT FAILURE state after 5s.")
	}
	cancel()
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if ok := cc.WaitForStateChange(ctx, connectivity.Ready); !ok {
		t.Fatal("ClientConn is still in READY state after 5s.")
	}
	cancel()
	if s := cc.GetState(); s != connectivity.TransientFailure {
		t.Fatalf("ClientConn is in %v state, want TRANSIENT FAILURE", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if ok := cc.WaitForStateChange(ctx, connectivity.TransientFailure); !ok {
		t.Fatal("ClientConn is still in TRANSIENT FAILURE state after 5s.")
	}
	cancel()
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_UNKNOWN)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if ok := cc.WaitForStateChange(ctx, connectivity.TransientFailure); !ok {
		t.Fatal("ClientConn is still in TRANSIENT FAILURE state after 5s.")
	}
	cancel()
	if s := cc.GetState(); s != connectivity.Ready {
		t.Fatalf("ClientConn is in %v state, want READY", s)
	}
}

// In the case of a goaway received, the health check stream should be terminated and health check
// function should exit.
func TestHealthCheckWithGoAway(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}
	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()

	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// the stream rpc will persist through goaway event.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := tc.FullDuplexCall(ctx, grpc.FailFast(false))
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
		t.Fatal("Health check function has not exited after 5s.")
	}

	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
	// The RPC will run until canceled.
	cancel()
}

func TestHealthCheckWithConnClose(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()
	tc := testpb.NewTestServiceClient(cc)

	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
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
		t.Fatal("Health check function has not exited after 5s.")
	}
}

// addrConn drain happens when addrConn gets tron down due to its address being no longer in the
// address list returned by the resolver.
func TestHealthCheckWithAddrConnDrain(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	// the stream rpc will persist through goaway event.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := tc.FullDuplexCall(ctx, grpc.FailFast(false))
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
	r.NewAddress([]resolver.Address{})

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not exited after 5s.")
	}

	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
	// The RPC will run until canceled.
	cancel()
}

// ClientConn close will lead to its addrConns being torn down.
func TestHealthCheckWithClientConnClose(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

	// make some rpcs to make sure connection is working.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
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
		t.Fatal("Health check function has not exited after 5s.")
	}
}

// This test is to test the logic in the createTransport after the health check function returns which
// closes the skipReset channel(since it has not been closed inside health check func) to unblock
// onGoAway/onClose goroutine.
func TestHealthCheckWithoutReportHealthCalledAddrConnShutDown(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("delay", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan := make(chan struct{})
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	// The serviceName "delay" is specially handled at server side, where response will not be sent
	// back to client immediately upon receiving the request (client should receive no response until
	// test ends).
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "delay"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

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
	r.NewAddress([]resolver.Address{})

	// The health check func should exit without calling the reportHealth func, as server hasn't sent
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
func TestHealthCheckWithoutReportHealthCalled(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("delay", healthpb.HealthCheckResponse_SERVING)

	hcEnterChan := make(chan struct{})
	hcExitChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		err := testHealthCheckFunc(ctx, newStream, update, service)
		close(hcExitChan)
		return err
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	defer cc.Close()

	// The serviceName "delay" is specially handled at server side, where response will not be sent
	// back to client immediately upon receiving the request (client should receive no response until
	// test ends).
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "delay"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

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

	// The health check func should exit without calling the reportHealth func, as server hasn't sent
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
	hcEnterChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		return nil
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"), grpc.WithDisableHealthCheck())
	if err != nil {
		t.Fatal("dial failed")
	}
	tc := testpb.NewTestServiceClient(cc)
	defer cc.Close()
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: addr}})

	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
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
	hcEnterChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		return nil
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("pick_first"))
	if err != nil {
		t.Fatal("dial failed")
	}
	tc := testpb.NewTestServiceClient(cc)
	defer cc.Close()
	r.NewServiceConfig(`{
	"healthCheckConfig": {
		"serviceName": "foo"
	}
}`)
	r.NewAddress([]resolver.Address{{Addr: addr}})

	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
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
	hcEnterChan := make(chan struct{})
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		return nil
	}

	replace := replaceHealthCheckFunc(testHealthCheckFuncWrapper)
	defer replace()
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()
	cc, err := grpc.Dial(r.Scheme()+":///test.server", grpc.WithInsecure(), grpc.WithBalancerName("round_robin"))
	if err != nil {
		t.Fatal("dial failed")
	}
	tc := testpb.NewTestServiceClient(cc)
	defer cc.Close()

	r.NewAddress([]resolver.Address{{Addr: addr}})

	// send some rpcs to make sure transport has been created and is ready for use.
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
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

func TestHealthCheckDisable(t *testing.T) {
	defer leakcheck.Check(t)
	// set up server side
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	ts := newTestHealthServer()
	healthpb.RegisterHealthServer(s, ts)
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	// test client side disabling configuration.
	testHealthCheckDisableWithDialOption(t, lis.Addr().String())
	testHealthCheckDisableWithBalancer(t, lis.Addr().String())
	testHealthCheckDisableWithServiceConfig(t, lis.Addr().String())
}
