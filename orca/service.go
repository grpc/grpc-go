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
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/status"

	v3orcaservicegrpc "github.com/cncf/xds/go/xds/service/orca/v3"
	v3orcaservicepb "github.com/cncf/xds/go/xds/service/orca/v3"
)

// Service provides an implementation of the OpenRcaService as defined in the
// [ORCA] service protos.
//
// Server applications can use the SetXxx() methods to record measurements
// corresponding to backend metrics, which eventually get pushed to clients who
// have initiated the SteamCoreMetrics streaming RPC.
//
// [ORCA]: https://github.com/cncf/xds/blob/main/xds/service/orca/v3/orca.proto
type Service struct {
	v3orcaservicegrpc.UnimplementedOpenRcaServiceServer

	minReportingInterval time.Duration
	recorder             *metricRecorder
}

// ServiceOptions contains options to configure the ORCA service implementation.
type ServiceOptions struct {
	// MinReportingInterval sets the lower bound for how often out-of-band
	// metrics are reported on the streaming RPC initiated by the client.  If
	// unspecified or less than the default value of 30s, the default is used.
	// Clients may request a higher value as part of the StreamCoreMetrics
	// streaming RPC.
	MinReportingInterval time.Duration
}

// Register creates a new ORCA service implementation configured using the
// provided options and registers the same on the provided service registrar.
func Register(s grpc.ServiceRegistrar, opts ServiceOptions) (*Service, error) {
	// TODO(easwars): Once the generated pb.gos are updated, we wont need this
	// type assertion any more, since the new versions accept a
	// grpc.ServiceRegistrar in their registration functions.
	srv, ok := s.(*grpc.Server)
	if !ok {
		return nil, fmt.Errorf("concrete type of provided grpc.ServiceRegistrar is %T, only supported type is %T", s, &grpc.Server{})
	}

	if opts.MinReportingInterval < internal.MinORCAReportingInterval {
		opts.MinReportingInterval = internal.MinORCAReportingInterval
	}
	server := &Service{
		minReportingInterval: opts.MinReportingInterval,
		recorder:             newMetricRecorder(),
	}
	v3orcaservicegrpc.RegisterOpenRcaServiceServer(srv, server)
	return server, nil
}

func (s *Service) determineReportingInterval(req *v3orcaservicegrpc.OrcaLoadReportRequest) time.Duration {
	if req.GetReportInterval() == nil {
		return s.minReportingInterval
	}
	interval := req.GetReportInterval()
	if err := interval.CheckValid(); err != nil {
		logger.Warningf("Received reporting interval %q is invalid: %v. Using default: %s", interval, s.minReportingInterval)
		return s.minReportingInterval
	}
	if interval.AsDuration() < s.minReportingInterval {
		logger.Warningf("Received reporting interval %q is lesser than configured minimum: %v. Using default: %s", interval, s.minReportingInterval)
		return s.minReportingInterval
	}
	return interval.AsDuration()
}

func (s *Service) sendMetricsResponse(stream v3orcaservicegrpc.OpenRcaService_StreamCoreMetricsServer) error {
	return stream.Send(s.recorder.toLoadReportProto())
}

// StreamCoreMetrics streams custom backend metrics injected by the server
// application.
func (s *Service) StreamCoreMetrics(req *v3orcaservicepb.OrcaLoadReportRequest, stream v3orcaservicepb.OpenRcaService_StreamCoreMetricsServer) error {
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

// SetUtilizationMetric records a measurement for a utilization metric uniquely
// identifiable by name.
func (s *Service) SetUtilizationMetric(name string, val float64) {
	s.recorder.setUtilization(name, val)
}

// DeleteUtilizationMetric deletes any previously recorded measurement for a
// utilization metric uniquely identifiable by name.
func (s *Service) DeleteUtilizationMetric(name string) {
	s.recorder.deleteUtilization(name)
}

// SetAllUtilizationMetrics records a measurement for a bunch of utilization
// metrics specified by the provided map, where keys correspond to metric names
// and values to measurements.
func (s *Service) SetAllUtilizationMetrics(pairs map[string]float64) {
	s.recorder.setAllUtilization(pairs)
}

// SetCPUUtilizationMetric records a measurement for the CPU utilization metric.
func (s *Service) SetCPUUtilizationMetric(val float64) {
	s.recorder.setCPU(val)
}

// DeleteCPUUtilizationMetric deletes a previously recorded measurement for the
// CPU utilization metric.
func (s *Service) DeleteCPUUtilizationMetric() {
	s.recorder.setCPU(0)
}

// SetMemoryUtilizationMetric records a measurement for the memory utilization
// metric.
func (s *Service) SetMemoryUtilizationMetric(val float64) {
	s.recorder.setMemory(val)
}

// DeleteMemoryUtilizationMetric deletes a previously recorded measurement for
// the memory utilization metric.
func (s *Service) DeleteMemoryUtilizationMetric() {
	s.recorder.setMemory(0)
}
