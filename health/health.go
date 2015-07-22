// Package health provides some utility functions to health-check a server. The implementation
// is based on protobuf. Users need to write their own implementations if other IDLs are used.
package health

import (
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1alpha"
)

// HealthCheck is the client side function to health-check a server
func HealthCheck(t time.Duration, cc *grpc.ClientConn, service_name string) (*healthpb.HealthCheckResponse, error) {
	ctx, _ := context.WithTimeout(context.Background(), t)
	hc := healthpb.NewHealthCheckClient(cc)
	req := new(healthpb.HealthCheckRequest)
	req.Host = ""
	req.Service = service_name
	out, err := hc.Check(ctx, req)
	return out, err
}

type HealthServer struct {
	StatusMap map[string]int32
}

func (s *HealthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (out *healthpb.HealthCheckResponse, err error) {
	service := ":" + in.Service
	out = new(healthpb.HealthCheckResponse)
	status, ok := s.StatusMap[service]
	out.Status = healthpb.HealthCheckResponse_ServingStatus(status)
	if !ok {
		err = grpc.Errorf(codes.NotFound, "unknown service")
	} else {
		err = nil
	}
	return out, err
}

func (s *HealthServer) SetServingStatus(service string, status int32) {
	service = ":" + service
	s.StatusMap[service] = status
}
