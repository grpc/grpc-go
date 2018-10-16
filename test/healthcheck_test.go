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
	fmt.Println("set hc func", internal.HealthCheckFunc != nil)
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
			//stream should have been closed, so we can create safely continue to create a new stream.
			continue
		}
		s.CloseSend()
		for {
			resp := new(healthpb.HealthCheckResponse)
			if err = s.RecvMsg(resp); err != nil {
				fmt.Println("RecvMsg failed with err:", err)
				// we can safely break here and continue to create a new stream, since a non-nil error has been received.
				break
			}
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
	}
}

type testHealthServer struct {
	mu     sync.Mutex
	status healthpb.HealthCheckResponse_ServingStatus
	update chan struct{}
}

// Check implements `service Health`.
func (s *testHealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	return &healthpb.HealthCheckResponse{
		Status: healthpb.HealthCheckResponse_SERVING,
	}, nil
}

// Watch implements `service Health`.
func (s *testHealthServer) Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	fmt.Println("enter watch")
	switch in.Service {
	case "foo":
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
				Status: s.status,
			}
			s.mu.Unlock()
			stream.SendMsg(resp)
		}
		return nil
	default:
		return status.Error(codes.Unimplemented, "the provided service name is not supported")
	}
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap.
func (s *testHealthServer) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	s.mu.Lock()
	s.status = status
	fmt.Println("lallalallalallal")
	select {
	case <-s.update:
	default:
	}
	s.update <- struct{}{}
	fmt.Println("hh1")

	s.mu.Unlock()
}

func TestHealthCheckWatch(t *testing.T) {
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
	ts.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
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
	for i := 0; i < 10; i++ {
		if state := cc.GetState(); state == connectivity.Ready {
			t.Fatal("should not be ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ts.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	var i int
	for i = 0; i < 100; i++ {
		if state := cc.GetState(); state == connectivity.Ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if i == 100 {
		t.Fatal("should be ready")
	}

	ts.SetServingStatus("", healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	for i = 0; i < 100; i++ {
		if state := cc.GetState(); state == connectivity.TransientFailure || state == connectivity.Connecting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if i == 100 {
		t.Fatal("should be transient failure or connecting")
	}
}

// Goal: In the case of a goaway happens health check stream should be ended and health check func exited.
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
	ts.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
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
	if err := verifyResultWithDelay(func() (bool, error) {
		if _, err := tc.EmptyCall(context.Background(), &testpb.Empty{}); err != nil {
			return false, fmt.Errorf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

func TestHealthCheckWithClose(t *testing.T) {

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
