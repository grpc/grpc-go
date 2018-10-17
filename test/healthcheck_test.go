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
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func replaceHealthCheckFunc(f func(context.Context, func() (interface{}, error), func(bool), string) error) func() {
	oldHcFunc := internal.HealthCheckFunc
	internal.HealthCheckFunc = f
	return func() {
		internal.HealthCheckFunc = oldHcFunc
	}
}

func testHealthCheckFunc(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		rawS, err := newStream()
		if err != nil {
			fmt.Println("newStream failed with err:", err)
			continue
		}
		s, ok := rawS.(grpc.ClientStream)
		if !ok {
			// exit the health check function
			return errors.New("type assertion to grpc.ClientStream failed")
		}
		if err = s.SendMsg(&healthpb.HealthCheckRequest{
			Service: service,
		}); err != nil && err != io.EOF {
			fmt.Println("SendMsg failed with err", err)
			//stream should have been closed, so we can safely continue to create a new stream.
			continue
		}
		s.CloseSend()
		for {
			resp := new(healthpb.HealthCheckResponse)
			if err = s.RecvMsg(resp); err != nil {
				fmt.Println("RecvMsg failed with err:", err)
				if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
					update(true)
					return err
				}
				// we can safely break here and continue to create a new stream, since a non-nil error has been received.
				break
			}
			fmt.Println("status", resp.Status)
			switch resp.Status {
			case healthpb.HealthCheckResponse_SERVING:
				fmt.Println("serving!")
				update(true)
			case healthpb.HealthCheckResponse_SERVICE_UNKNOWN, healthpb.HealthCheckResponse_UNKNOWN, healthpb.HealthCheckResponse_NOT_SERVING:
				fmt.Println("not serving")
				update(false)
			}
		}
		time.Sleep(100 * time.Millisecond)
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
		fmt.Println("inside foo")
		var done bool
		for {
			fmt.Println("enter for")
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
		// do nothing to mock a delay of health check response from server side.
		select {
		case <-stream.Context().Done():
		case <-time.After(3 * time.Second):
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
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_NOT_SERVING)
	replace := replaceHealthCheckFunc(testHealthCheckFunc)
	defer replace()
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

	// TODO: better way to make sure cc state is not in ready?
	for i := 0; i < 10; i++ {
		if state := cc.GetState(); state == connectivity.Ready {
			t.Fatal("ClientConn should not be in READY state")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVING)

	var i int
	for i = 0; i < 100; i++ {
		if state := cc.GetState(); state == connectivity.Ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if i == 100 {
		t.Fatal("ClientConn should be in READY state")
	}

	ts.SetServingStatus("foo", healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	for i = 0; i < 100; i++ {
		if state := cc.GetState(); state == connectivity.TransientFailure || state == connectivity.Connecting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if i == 100 {
		t.Fatal("ClientConn should be in TRANSIENT FAILURE or CONNECTING state")
	}
}

// In the case of a goaway happens, health check stream should be ended and health check func should exit.
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
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(100))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
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
	case <-time.After(time.Second):
		t.Fatal("Health check function has not exited after 1s.")
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
	case <-time.After(time.Second):
		t.Fatal("Health check function has not exited after 1s.")
	}
}

func TestHealthCheckWithAddrConnTearDown(t *testing.T) {
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
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(100))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
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
	case <-time.After(time.Second):
		t.Fatal("Health check function has not exited after 1s.")
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

func TestHealthCheckWithoutUpdateBeingCalled(t *testing.T) {
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
	case <-time.After(2 * time.Second):
		t.Fatal("Health check function has not been invoked after 2s.")
	}
	// trigger teardown of the ac
	r.NewAddress([]resolver.Address{})

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not exited after 1s.")
	}
}

func TestHealthCheckWithUnimplementedServerSide(t *testing.T) {
	defer leakcheck.Check(t)
	s := grpc.NewServer()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal("failed to listen")
	}
	testpb.RegisterTestServiceServer(s, &testServer{})
	go s.Serve(lis)
	defer s.Stop()
	hcExitChan := make(chan struct{})
	errChan := make(chan error, 1)
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		err := testHealthCheckFunc(ctx, newStream, update, service)
		errChan <- err
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
	case <-time.After(time.Second):
		t.Fatal("Health check function has not exited after 1s.")
	}
	select {
	case err := <-errChan:
		if s, ok := status.FromError(err); !ok || s.Code() != codes.Unimplemented {
			t.Fatalf("Health check service should have received error with code Unimplemented, received err : %v ", err)
		}
	default:
		t.Fatal("no err in the errChan")
	}
}

func TestHealthCheckDisableWithServiceConfig(t *testing.T) {
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
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		err := testHealthCheckFunc(ctx, newStream, update, service)
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
	tc := testpb.NewTestServiceClient(cc)
	defer cc.Close()

	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

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

func TestHealthCheckWithoutUpdateCalled(t *testing.T) {
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
	case <-time.After(2 * time.Second):
		t.Fatal("Health check function has not been invoked after 2s.")
	}
	// trigger teardown of the ac
	r.NewAddress([]resolver.Address{})

	select {
	case <-hcExitChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Health check function has not exited after 1s.")
	}
}

func TestHealthCheckDisableWithDialOption(t *testing.T) {
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
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		err := testHealthCheckFunc(ctx, newStream, update, service)
		return err
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
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

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

func TestHealthCheckDisableWithBalancer(t *testing.T) {
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
	testHealthCheckFuncWrapper := func(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
		close(hcEnterChan)
		err := testHealthCheckFunc(ctx, newStream, update, service)
		return err
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
	r.NewAddress([]resolver.Address{{Addr: lis.Addr().String()}})

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

// TODO: all tests
// 1. disable configurations
// 2. goaway
// 3. onclose
// 4. teardown
// 5. close chan when necessary
// 6. unimplemented
// 7. transfer to connecting state when recreating the stream
// 8.
