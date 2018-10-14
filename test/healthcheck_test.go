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
)

func replaceHealthCheckFunc(f interface{}) func() {
	oldHcFunc := internal.HealthCheckFunc
	internal.HealthCheckFunc = f
	fmt.Println("set hc func", internal.HealthCheckFunc)
	return func() {
		internal.HealthCheckFunc = oldHcFunc
	}
}

func testHealthCheckFunc(ctx context.Context, newStream func() (grpc.ClientStream, error), update func(bool), service string) error {
	for {
		select {
		case <-ctx.Done():
			return errors.New("exit")
		default:
		}
		s, err := newStream()
		if err != nil {
			fmt.Println("err 54", err)
			continue
		}
		if err = s.SendMsg(&healthpb.HealthCheckRequest{
			Service: service,
		}); err != nil {
			fmt.Println("err 60")
			continue
		}
		s.CloseSend()
		for {
			resp := new(healthpb.HealthCheckResponse)
			if err = s.RecvMsg(resp); err != nil {
				fmt.Println("err 65", err)
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
