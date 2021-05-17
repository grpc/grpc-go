/*
 *
 * Copyright 2020 gRPC authors.
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

// Package e2e provides utilities for end2end testing of xDS functionality.
package e2e

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strconv"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	v3cache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	v3server "github.com/envoyproxy/go-control-plane/pkg/server/v3"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

var logger = grpclog.Component("xds-e2e")

// serverLogger implements the Logger interface defined at
// envoyproxy/go-control-plane/pkg/log. This is passed to the Snapshot cache.
type serverLogger struct{}

func (l serverLogger) Debugf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.InfoDepth(1, msg)
}
func (l serverLogger) Infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.InfoDepth(1, msg)
}
func (l serverLogger) Warnf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.WarningDepth(1, msg)
}
func (l serverLogger) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	logger.ErrorDepth(1, msg)
}

// ManagementServer is a thin wrapper around the xDS control plane
// implementation provided by envoyproxy/go-control-plane.
type ManagementServer struct {
	// Address is the host:port on which the management server is listening for
	// new connections.
	Address string

	cancel  context.CancelFunc    // To stop the v3 ADS service.
	xs      v3server.Server       // v3 implementation of ADS.
	gs      *grpc.Server          // gRPC server which exports the ADS service.
	cache   v3cache.SnapshotCache // Resource snapshot.
	version int                   // Version of resource snapshot.
}

// StartManagementServer initializes a management server which implements the
// AggregatedDiscoveryService endpoint. The management server is initialized
// with no resources. Tests should call the Update() method to change the
// resource snapshot held by the management server, as required by the test
// logic. When the test is done, it should call the Stop() method to cleanup
// resources allocated by the management server.
func StartManagementServer() (*ManagementServer, error) {
	// Create a snapshot cache.
	cache := v3cache.NewSnapshotCache(true, v3cache.IDHash{}, serverLogger{})
	logger.Infof("Created new snapshot cache...")

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start xDS management server: %v", err)
	}

	// Create an xDS management server and register the ADS implementation
	// provided by it on a gRPC server. Cancelling the context passed to the
	// server is the only way of stopping it at the end of the test.
	ctx, cancel := context.WithCancel(context.Background())
	xs := v3server.NewServer(ctx, cache, v3server.CallbackFuncs{})
	gs := grpc.NewServer()
	v3discoverygrpc.RegisterAggregatedDiscoveryServiceServer(gs, xs)
	logger.Infof("Registered Aggregated Discovery Service (ADS)...")

	// Start serving.
	go gs.Serve(lis)
	logger.Infof("xDS management server serving at: %v...", lis.Addr().String())

	return &ManagementServer{
		Address: lis.Addr().String(),
		cancel:  cancel,
		version: 0,
		gs:      gs,
		xs:      xs,
		cache:   cache,
	}, nil
}

// UpdateOptions wraps parameters to be passed to the Update() method.
type UpdateOptions struct {
	// NodeID is the id of the client to which this update is to be pushed.
	NodeID string
	// Endpoints, Clusters, Routes, and Listeners are the updated list of xds
	// resources for the server.  All must be provided with each Update.
	Endpoints []*v3endpointpb.ClusterLoadAssignment
	Clusters  []*v3clusterpb.Cluster
	Routes    []*v3routepb.RouteConfiguration
	Listeners []*v3listenerpb.Listener
	// SkipValidation indicates whether we want to skip validation (by not
	// calling snapshot.Consistent()). It can be useful for negative tests,
	// where we send updates that the client will NACK.
	SkipValidation bool
}

// Update changes the resource snapshot held by the management server, which
// updates connected clients as required.
func (s *ManagementServer) Update(opts UpdateOptions) error {
	s.version++

	// Create a snapshot with the passed in resources.
	snapshot := v3cache.NewSnapshot(strconv.Itoa(s.version), resourceSlice(opts.Endpoints), resourceSlice(opts.Clusters), resourceSlice(opts.Routes), resourceSlice(opts.Listeners), nil /*runtimes*/, nil /*secrets*/)
	if !opts.SkipValidation {
		if err := snapshot.Consistent(); err != nil {
			return fmt.Errorf("failed to create new resource snapshot: %v", err)
		}
	}
	logger.Infof("Created new resource snapshot...")

	// Update the cache with the new resource snapshot.
	if err := s.cache.SetSnapshot(opts.NodeID, snapshot); err != nil {
		return fmt.Errorf("failed to update resource snapshot in management server: %v", err)
	}
	logger.Infof("Updated snapshot cache with resource snapshot...")
	return nil
}

// Stop stops the management server.
func (s *ManagementServer) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.gs.Stop()
}

// resourceSlice accepts a slice of any type of proto messages and returns a
// slice of types.Resource.  Will panic if there is an input type mismatch.
func resourceSlice(i interface{}) []types.Resource {
	v := reflect.ValueOf(i)
	rs := make([]types.Resource, v.Len())
	for i := 0; i < v.Len(); i++ {
		rs[i] = v.Index(i).Interface().(types.Resource)
	}
	return rs
}
