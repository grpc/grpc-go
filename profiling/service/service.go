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

// The service package defines methods to register a gRPC client/service for a
// profiling service that is exposed in the same server. This service can be
// queried by a client to remotely manage the gRPC profiling behaviour of an
// application.
package service

import (
	"errors"
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"google.golang.org/grpc/internal/profiling"
	ppb "google.golang.org/grpc/profiling/proto"
	pspb "google.golang.org/grpc/profiling/proto/service"
)

// Profiling configuration options.
type ProfilingConfig struct {
	// Setting this to true will enable profiling.
	Enabled bool

	// Profiling uses a circular buffer (ring buffer) to store statistics for
	// only the last few RPCs so that profiling stats do not grow unbounded. This
	// parameter defines the upper limit on the number of RPCs for which
	// statistics should be stored at any given time. An average RPC requires
	// approximately 2-3 KiB of memory for profiling-related statistics, so
	// choose an appropriate number based on the amount of memory you can afford.
	StreamStatsSize uint32

	// To expose the profiling service and its methods, a *grpc.Server must be
	// provided.
	Server *grpc.Server
}

var errorNilServer = errors.New("No grpc.Server provided.")

// Init takes a *ProfilingConfig to initialise profiling (turned on/off
// depending on the value set in pc.Enabled) and register the profiling service
// in the server provided in pc.Server.
func Init(pc *ProfilingConfig) error {
	if pc.Server == nil {
		return errorNilServer
	}

	if err := profiling.InitStats(pc.StreamStatsSize); err != nil {
		return err
	}

	pspb.RegisterProfilingServer(pc.Server, &profilingServer{})

	// Do this last after everything has been initialised and allocated.
	profiling.SetEnabled(pc.Enabled)

	return nil
}

type profilingServer struct{}

func (s *profilingServer) Enable(ctx context.Context, req *pspb.EnableRequest) (*pspb.EnableResponse, error) {
	if req.Enabled {
		grpclog.Infof("Enabling profiling")
	} else {
		grpclog.Infof("Disabling profiling")
	}
	profiling.SetEnabled(req.Enabled)

	return &pspb.SetEnabledResponse{}, nil
}

func (s *profilingServer) GetStreamStats(req *pspb.GetStreamStatsRequest, stream pspb.Profiling_GetStreamStatsServer) error {
	grpclog.Infof("Processing stream request for stream stats")
	results := profiling.StreamStats.Drain()
	grpclog.Infof("Stream stats size: %v records", len(results))

	for i := 0; i < len(results); i++ {
		if err := stream.Send(ppb.StatToStatProto(results[i].(*profiling.Stat))); err != nil {
			return err
		}
	}

	return nil
}
