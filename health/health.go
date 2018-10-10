/*
 *
 * Copyright 2017 gRPC authors.
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

//go:generate ./regenerate.sh

// Package health provides some utility functions to health-check a server. The implementation
// is based on protobuf. Users need to write their own implementations if other IDLs are used.
package health

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

// Server implements `service Health`.
type Server struct {
	mu sync.Mutex
	// statusMap stores the serving status of the services this Server monitors.
	statusMap map[string]healthpb.HealthCheckResponse_ServingStatus
	listeners map[string]map[healthpb.Health_WatchServer]chan struct{}
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{
		statusMap: make(map[string]healthpb.HealthCheckResponse_ServingStatus),
		listeners: make(map[string]map[healthpb.Health_WatchServer]chan struct{}),
	}
}

// Check implements `service Health`.
func (s *Server) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.Service == "" {
		// check the server overall health status.
		return &healthpb.HealthCheckResponse{
			Status: healthpb.HealthCheckResponse_SERVING,
		}, nil
	}
	if status, ok := s.statusMap[in.Service]; ok {
		return &healthpb.HealthCheckResponse{
			Status: status,
		}, nil
	}
	return nil, status.Error(codes.NotFound, "unknown service")
}

// Watch implements `service Health`.
func (s *Server) Watch(in *healthpb.HealthCheckRequest, stream healthpb.Health_WatchServer) error {
	service := in.Service
	listener := make(chan struct{})
	s.mu.Lock()
	if _, ok := s.listeners[service]; !ok {
		s.listeners[service] = make(map[healthpb.Health_WatchServer]chan struct{})
	}
	s.listeners[service][stream] = listener
	s.mu.Unlock()
	for {
		s.mu.Lock()
		stream.Send(&healthpb.HealthCheckResponse{Status: s.statusMap[service]})
		s.mu.Unlock()
		select {
		case <-listener:
		case <-stream.Context().Done():
			s.mu.Lock()
			delete(s.listeners[service], stream)
			s.mu.Unlock()
			return status.Error(codes.Canceled, "Stream has ended.")
		}
	}
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap.
func (s *Server) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	s.mu.Lock()
	s.statusMap[service] = status
	for _, listener := range s.listeners[service] {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
	s.mu.Unlock()
}
