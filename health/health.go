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
	// StatusMap stores the serving status of a service
	statusMap map[string]int32
}

func NewHealthServer() *HealthServer {
	return &HealthServer{
		statusMap: make(map[string]int32),
	}
}

func (s *HealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (out *healthpb.HealthCheckResponse, err error) {
	service := in.Host + ":" + in.Service
	out = new(healthpb.HealthCheckResponse)
	status, ok := s.statusMap[service]
	out.Status = healthpb.HealthCheckResponse_ServingStatus(status)
	if !ok {
		err = grpc.Errorf(codes.NotFound, "unknown service")
	} else {
		err = nil
	}
	return out, err
}

// SetServingStatus is called when need to reset the serving status of a service
// or insert a new service entry into the statusMap
func (s *HealthServer) SetServingStatus(host string, service string, status int32) {
	service = host + ":" + service
	var mu sync.Mutex
	mu.Lock()
	s.statusMap[service] = status
	mu.Unlock()
}
