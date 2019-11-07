/*
 *
 * Copyright 2019 gRPC authors.
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
	Enabled         bool
	StreamStatsSize uint32
	Server          *grpc.Server
}

func registerService(s *grpc.Server) {
	grpclog.Infof("registering profiling service")
	pspb.RegisterProfilingServer(s, &profilingServer{})
}

func Init(pc *ProfilingConfig) (err error) {
	if err = profiling.InitStats(pc.StreamStatsSize); err != nil {
		return
	}

	registerService(pc.Server)

	// Do this last after everything has been initialised and allocated.
	profiling.SetEnabled(pc.Enabled)

	return
}

type profilingServer struct{}

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
