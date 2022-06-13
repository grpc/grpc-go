/*
 *
 * Copyright 2022 gRPC authors.
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

package orca

import (
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v3orcaservicegrpc "github.com/cncf/xds/go/xds/service/orca/v3"
	v3orcaservicepb "github.com/cncf/xds/go/xds/service/orca/v3"
)

// serviceImpl provides an implementation of the OpenRcaService as defined in
// the [ORCA] service protos.
//
// [ORCA]: https://github.com/cncf/xds/blob/main/xds/service/orca/v3/orca.proto
type serviceImpl struct {
	v3orcaservicegrpc.UnimplementedOpenRcaServiceServer

	minReportingInterval time.Duration
}

func newServiceImpl(opts OutOfBandMetricsReportingOptions) *serviceImpl {
	return &serviceImpl{minReportingInterval: opts.MinReportingInterval}
}

func (s *serviceImpl) determineReportingInterval(req *v3orcaservicegrpc.OrcaLoadReportRequest) time.Duration {
	if req.GetReportInterval() == nil {
		return s.minReportingInterval
	}
	interval := req.GetReportInterval()
	if err := interval.CheckValid(); err != nil {
		logger.Warningf("Received reporting interval %q is invalid: %v. Using default: %s", interval, s.minReportingInterval)
		return s.minReportingInterval
	}
	return interval.AsDuration()
}

func (s *serviceImpl) sendMetricsResponse(stream v3orcaservicegrpc.OpenRcaService_StreamCoreMetricsServer) error {
	// The oobRecorder global singleton is guaranteed to be initialized at this
	// point because registration of this service implementation and the
	// initialization of the former take place in the same function,
	// EnableOutOfBandMetricsReporting().
	resp := oobRecorder.recorder.toLoadReportProto()
	return stream.Send(resp)
}

func (s *serviceImpl) StreamCoreMetrics(req *v3orcaservicepb.OrcaLoadReportRequest, stream v3orcaservicepb.OpenRcaService_StreamCoreMetricsServer) error {
	// Send one response immediately before creating and waiting for the timer
	// to fire.
	if err := s.sendMetricsResponse(stream); err != nil {
		return err
	}

	ticker := time.NewTicker(s.determineReportingInterval(req))
	defer ticker.Stop()

	for {
		// Send a response containing the currently recorded metrics
		select {
		case <-stream.Context().Done():
			return status.Error(codes.Canceled, "Stream has ended.")
		case <-ticker.C:
			if err := s.sendMetricsResponse(stream); err != nil {
				return err
			}
		}
	}
}
