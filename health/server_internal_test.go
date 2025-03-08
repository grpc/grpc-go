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

package health

import (
	"context"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
	"testing"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestShutdown(t *testing.T) {
	const testService = "tteesstt"
	s := NewServer()
	s.SetServingStatus(testService, healthpb.HealthCheckResponse_SERVING)

	status := s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_SERVING)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Run SetServingStatus and Shutdown in parallel.
	go func() {
		for i := 0; i < 1000; i++ {
			s.SetServingStatus(testService, healthpb.HealthCheckResponse_SERVING)
			time.Sleep(time.Microsecond)
		}
		wg.Done()
	}()
	go func() {
		time.Sleep(300 * time.Microsecond)
		s.Shutdown()
		wg.Done()
	}()
	wg.Wait()

	s.mu.Lock()
	status = s.statusMap[testService]
	s.mu.Unlock()
	if status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_NOT_SERVING)
	}

	s.Resume()
	status = s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_SERVING)
	}

	s.SetServingStatus(testService, healthpb.HealthCheckResponse_NOT_SERVING)
	status = s.statusMap[testService]
	if status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status for %s is %v, want %v", testService, status, healthpb.HealthCheckResponse_NOT_SERVING)
	}
}

func (s) TestList(t *testing.T) {
	s := NewServer()

	// Remove the zero value
	delete(s.statusMap, "")

	// Fill out status map with service information, 101 elements will trigger an error.
	for i := 1; i <= 101; i++ {
		s.statusMap[fmt.Sprintf("%d", i)] = healthpb.HealthCheckResponse_SERVING
	}

	ctx := context.TODO()
	var in healthpb.HealthListRequest

	_, err := s.List(ctx, &in)

	if err == nil {
		t.Fatalf("List should have failed, got %s", err)
	}
	if !errors.Is(err, status.Error(codes.ResourceExhausted, "server health list exceeds maximum capacity (100)")) {
		t.Fatal("List should have failed with resource exhausted")
	}

	// Remove the 101 element to avoid the error.
	delete(s.statusMap, "101")

	// Retry getting the list, this time with a successful result.
	out, err := s.List(ctx, &in)
	if err != nil {
		t.Fatalf("List should not have failed, got %s, len %d", err, len(s.statusMap))
	}

	if len(out.GetStatuses()) != len(s.statusMap) {
		t.Fatal("List should have return the same number of elements as the inner status map")
	}

	for key := range out.GetStatuses() {
		if _, ok := s.statusMap[key]; !ok {
			t.Fatalf("List should have returned all resources, wanted %s, but it did not exist in the inner status map", key)
		}
	}
}
