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
	statusMap    map[string]healthpb.HealthCheckResponse_ServingStatus
	listenersMap map[string][]chan struct{}
}

// NewServer returns a new Server.
func NewServer() *Server {
	return &Server{
		statusMap:    make(map[string]healthpb.HealthCheckResponse_ServingStatus),
		listenersMap: make(map[string][]chan struct{}),
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
	var listener chan struct{}
	s.sendUpdate(in.Service, stream, &listener)
	for {
		select {
		case <-listener:
			s.sendUpdate(in.Service, stream, &listener)
		case <-stream.Context().Done():
			return status.Error(codes.Canceled, "Stream has ended.")
		}
	}
}

func (s *Server) sendUpdate(service string, stream healthpb.Health_WatchServer, listenerPtr *chan struct{}) {
	stream.Send(&healthpb.HealthCheckResponse{Status: s.statusMap[service]})
	*listenerPtr = make(chan struct{}, 1)
	s.mu.Lock()
	s.listenersMap[service] = append(s.listenersMap[service], *listenerPtr)
	s.mu.Unlock()
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap.
func (s *Server) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	s.mu.Lock()
	s.statusMap[service] = status
	listeners := s.listenersMap[service]
	s.listenersMap[service] = make([]chan struct{}, 0)
	s.mu.Unlock()
	for _, listener := range listeners {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
}
