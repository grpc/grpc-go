// Package health provides some utility functions to health-check a server. The implementation
// is based on protobuf. Users need to write their own implementations if other IDLs are used.
package health

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1alpha"
)

type HealthServer struct {
	mu sync.Mutex
	// statusMap stores the serving status of the services this HealthServer monitors.
	statusMap map[string]int32
}

func NewHealthServer() *HealthServer {
	return &HealthServer{
		statusMap: make(map[string]int32),
	}
}

func (s *HealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	service := in.Host + ":" + in.Service
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, ok := s.statusMap[service]; ok {
		return &healthpb.HealthCheckResponse{
			Status: healthpb.HealthCheckResponse_ServingStatus(status),
		}, nil
	}
	return nil, grpc.Errorf(codes.NotFound, "unknown service")
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap.
func (s *HealthServer) SetServingStatus(host string, service string, status int32) {
	service = host + ":" + service
	s.mu.Lock()
	s.statusMap[service] = status
	s.mu.Unlock()
}
