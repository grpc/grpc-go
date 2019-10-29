package service

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"google.golang.org/grpc/internal/profiling"
	ppb "google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
)

type ProfilingConfig struct {
	Enabled bool
	SampleCount uint32
	Server *grpc.Server
}

func registerService(s *grpc.Server) {
	grpclog.Infof("registering profiling service")
	pspb.RegisterProfilingServer(s, &profilingServer{})
}

func Init(pc *ProfilingConfig) (err error) {
	err = profiling.InitStats(pc.SampleCount)
	if err != nil {
		return
	}

	registerService(pc.Server)

	// Do this last after everything has been initialised and allocated.
	profiling.SetEnabled(pc.Enabled)

	return
}

type profilingServer struct {}

func (s *profilingServer) SetEnabled(ctx context.Context, req *pspb.SetEnabledRequest) (ser *pspb.SetEnabledResponse, err error) {
	grpclog.Infof("processing SetEnabled (%v)", req.Enabled)
	profiling.SetEnabled(req.Enabled)

	ser = &pspb.SetEnabledResponse{Success: true}
	err = nil
	return
}

func (s *profilingServer) GetStreamStats(req *pspb.GetStreamStatsRequest, stream pspb.Profiling_GetStreamStatsServer) (err error) {
	grpclog.Infof("processing stream request for stream stats")
	results := profiling.StreamStats.Drain()
	grpclog.Infof("stream stats size: %v records", len(results))

	enabled := profiling.IsEnabled()
	if enabled {
		profiling.SetEnabled(false)
	}

	for i := 0; i < len(results); i++ {
		if err = stream.Send(ppb.StatToStatProto(results[i].(*profiling.Stat))); err != nil {
			return
		}
	}

	if enabled {
		profiling.SetEnabled(true)
	}

	return
}
