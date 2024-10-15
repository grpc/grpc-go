/*
 *
 * Copyright 2024 gRPC authors.
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
 */

// Package lrs provides the implementation of an LRS (Load Reporting Service)
// stream for the xDS client.
package lrs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

// Any per-RPC level logs which print complete request or response messages
// should be gated at this verbosity level. Other per-RPC level logs which print
// terse output should be at `INFO` and verbosity 2.
const perRPCVerbosityLevel = 9

// Stream provides all the functionality associated with an LRS (Load Reporting
// Service) stream on the client-side.
type Stream struct {
	// The following fields are initialized when a Stream instance is created
	// and are read-only afterwards, and hence can be accessed without a mutex.
	transport transport.Interface     // Transport to use for LRS stream.
	backoff   func(int) time.Duration // Backoff for retries after stream failures.
	nodeProto *v3corepb.Node          // Identifies the gRPC application.
	doneCh    chan struct{}           // To notify exit of LRS goroutine.
	logger    *igrpclog.PrefixLogger

	// Guards access to the below fields.
	mu           sync.Mutex
	cancelStream context.CancelFunc // Cancel the stream. If nil, the stream is not active.
	refCount     int                // Number of interested parties.
	lrsStore     *load.Store        // Store returned to user for pushing loads.
}

// StreamOpts holds the options for creating an lrsStream.
type StreamOpts struct {
	Transport transport.Interface     // xDS transport to create the stream on.
	Backoff   func(int) time.Duration // Backoff after stream failures.
	NodeProto *v3corepb.Node          // Node proto to identify the gRPC application.
	LogPrefix string                  // Prefix to be used for log messages.
}

// NewStream creates a new LRS Stream with the provided options.
//
// The actual streaming RPC call is initiated when the first call to ReportLoad
// is made, and is terminated when the last call to ReportLoad is canceled.
func NewStream(opts StreamOpts) *Stream {
	lrs := &Stream{
		transport: opts.Transport,
		backoff:   opts.Backoff,
		nodeProto: opts.NodeProto,
		lrsStore:  load.NewStore(),
	}

	l := grpclog.Component("xds")
	lrs.logger = igrpclog.NewPrefixLogger(l, opts.LogPrefix+fmt.Sprintf("[lrs-stream %p] ", lrs))
	return lrs
}

// ReportLoad returns a load.Store that can be used to report load, and a
// cleanup function that should be called when the load reporting is no longer
// needed.
//
// The first call to ReportLoad sets the reference count to one, and starts the
// LRS streaming call. Subsequent calls increment the reference count and return
// the same load.Store.
//
// The cleanup function decrements the reference count and stops the LRS stream
// when the last reference is removed.
func (lrs *Stream) ReportLoad() (*load.Store, func()) {
	lrs.mu.Lock()
	defer lrs.mu.Unlock()

	cleanup := grpcsync.OnceFunc(func() {
		lrs.mu.Lock()
		defer lrs.mu.Unlock()

		if lrs.refCount == 0 {
			lrs.logger.Errorf("Attempting to stop an LRS stream that has already been stopped.")
			return
		}
		lrs.refCount--
		if lrs.refCount != 0 {
			return
		}
		lrs.cancelStream()
		lrs.cancelStream = nil
		lrs.logger.Infof("Stopping LRS stream")
	})

	if lrs.refCount != 0 {
		lrs.refCount++
		return lrs.lrsStore, cleanup
	}

	lrs.refCount++
	ctx, cancel := context.WithCancel(context.Background())
	lrs.cancelStream = cancel
	lrs.doneCh = make(chan struct{})
	go lrs.runner(ctx)
	return lrs.lrsStore, cleanup
}

// runner is responsible for managing the lifetime of an LRS streaming call. It
// creates the stream, sends the initial LoadStatsRequest, receives the first
// LoadStatsResponse, and then starts a goroutine to periodically send
// LoadStatsRequests. The runner will restart the stream if it encounters any
// errors.
func (lrs *Stream) runner(ctx context.Context) {
	defer close(lrs.doneCh)

	// This feature indicates that the client supports the
	// LoadStatsResponse.send_all_clusters field in the LRS response.
	node := proto.Clone(lrs.nodeProto).(*v3corepb.Node)
	node.ClientFeatures = append(node.ClientFeatures, "envoy.lrs.supports_send_all_clusters")

	runLoadReportStream := func() error {
		// streamCtx is created and canceled in case we terminate the stream
		// early for any reason, to avoid gRPC-Go leaking the RPC's monitoring
		// goroutine.
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		stream, err := lrs.transport.CreateStreamingCall(streamCtx, "/envoy.service.load_stats.v3.LoadReportingService/StreamLoadStats")
		if err != nil {
			lrs.logger.Warningf("Creating new LRS stream failed: %v", err)
			return nil
		}
		if lrs.logger.V(2) {
			lrs.logger.Infof("LRS stream created")
		}

		if err := lrs.sendFirstLoadStatsRequest(stream, node); err != nil {
			lrs.logger.Warningf("Sending first LRS request failed: %v", err)
			return nil
		}

		clusters, interval, err := lrs.recvFirstLoadStatsResponse(stream)
		if err != nil {
			lrs.logger.Warningf("Reading from LRS stream failed: %v", err)
			return nil
		}

		// We reset backoff state when we successfully receive at least one
		// message from the server.
		lrs.sendLoads(streamCtx, stream, clusters, interval)
		return backoff.ErrResetBackoff
	}
	backoff.RunF(ctx, runLoadReportStream, lrs.backoff)
}

// sendLoads is responsible for periodically sending load reports to the LRS
// server at the specified interval for the specified clusters, until the passed
// in context is canceled.
func (lrs *Stream) sendLoads(ctx context.Context, stream transport.StreamingCall, clusterNames []string, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
		if err := lrs.sendLoadStatsRequest(stream, lrs.lrsStore.Stats(clusterNames)); err != nil {
			lrs.logger.Warningf("Writing to LRS stream failed: %v", err)
			return
		}
	}
}

func (lrs *Stream) sendFirstLoadStatsRequest(stream transport.StreamingCall, node *v3corepb.Node) error {
	req := &v3lrspb.LoadStatsRequest{Node: node}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Sending initial LoadStatsRequest: %s", pretty.ToJSON(req))
	}
	err := stream.Send(req)
	if err == io.EOF {
		return getStreamError(stream)
	}
	return err
}

// recvFirstLoadStatsResponse receives the first LoadStatsResponse from the LRS
// server.  Returns the following:
//   - a list of cluster names requested by the server or an empty slice if the
//     server requested for load from all clusters
//   - the load reporting interval, and
//   - any error encountered
//
// If the server requests for endpoint-level load reporting, an error is
// returned, since this is not yet supported.
func (lrs *Stream) recvFirstLoadStatsResponse(stream transport.StreamingCall) ([]string, time.Duration, error) {
	r, err := stream.Recv()
	if err != nil {
		return nil, 0, fmt.Errorf("lrs: failed to receive first LoadStatsResponse: %v", err)
	}
	resp, ok := r.(*v3lrspb.LoadStatsResponse)
	if !ok {
		return nil, time.Duration(0), fmt.Errorf("lrs: unexpected message type %T", r)
	}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Received first LoadStatsResponse: %s", pretty.ToJSON(resp))
	}

	rInterval := resp.GetLoadReportingInterval()
	if rInterval.CheckValid() != nil {
		return nil, 0, fmt.Errorf("lrs: invalid load_reporting_interval: %v", err)
	}
	interval := rInterval.AsDuration()

	if resp.ReportEndpointGranularity {
		// TODO(easwars): Support per endpoint loads.
		return nil, 0, errors.New("lrs: endpoint loads requested, but not supported by current implementation")
	}

	clusters := resp.Clusters
	if resp.SendAllClusters {
		// Return an empty slice to send stats for all clusters.
		clusters = []string{}
	}

	return clusters, interval, nil
}

func (lrs *Stream) sendLoadStatsRequest(stream transport.StreamingCall, loads []*load.Data) error {
	clusterStats := make([]*v3endpointpb.ClusterStats, 0, len(loads))
	for _, sd := range loads {
		droppedReqs := make([]*v3endpointpb.ClusterStats_DroppedRequests, 0, len(sd.Drops))
		for category, count := range sd.Drops {
			droppedReqs = append(droppedReqs, &v3endpointpb.ClusterStats_DroppedRequests{
				Category:     category,
				DroppedCount: count,
			})
		}
		localityStats := make([]*v3endpointpb.UpstreamLocalityStats, 0, len(sd.LocalityStats))
		for l, localityData := range sd.LocalityStats {
			lid, err := internal.LocalityIDFromString(l)
			if err != nil {
				return err
			}
			loadMetricStats := make([]*v3endpointpb.EndpointLoadMetricStats, 0, len(localityData.LoadStats))
			for name, loadData := range localityData.LoadStats {
				loadMetricStats = append(loadMetricStats, &v3endpointpb.EndpointLoadMetricStats{
					MetricName:                    name,
					NumRequestsFinishedWithMetric: loadData.Count,
					TotalMetricValue:              loadData.Sum,
				})
			}
			localityStats = append(localityStats, &v3endpointpb.UpstreamLocalityStats{
				Locality: &v3corepb.Locality{
					Region:  lid.Region,
					Zone:    lid.Zone,
					SubZone: lid.SubZone,
				},
				TotalSuccessfulRequests: localityData.RequestStats.Succeeded,
				TotalRequestsInProgress: localityData.RequestStats.InProgress,
				TotalErrorRequests:      localityData.RequestStats.Errored,
				TotalIssuedRequests:     localityData.RequestStats.Issued,
				LoadMetricStats:         loadMetricStats,
				UpstreamEndpointStats:   nil, // TODO: populate for per endpoint loads.
			})
		}

		clusterStats = append(clusterStats, &v3endpointpb.ClusterStats{
			ClusterName:           sd.Cluster,
			ClusterServiceName:    sd.Service,
			UpstreamLocalityStats: localityStats,
			TotalDroppedRequests:  sd.TotalDrops,
			DroppedRequests:       droppedReqs,
			LoadReportInterval:    durationpb.New(sd.ReportInterval),
		})
	}

	req := &v3lrspb.LoadStatsRequest{ClusterStats: clusterStats}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Sending LRS loads: %s", pretty.ToJSON(req))
	}
	err := stream.Send(req)
	if err == io.EOF {
		return getStreamError(stream)
	}
	return err
}

func getStreamError(stream transport.StreamingCall) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return err
		}
	}
}

// Stop blocks until the stream is closed and all spawned goroutines exit.
func (lrs *Stream) Stop() {
	lrs.mu.Lock()
	defer lrs.mu.Unlock()

	if lrs.cancelStream == nil {
		return
	}
	lrs.cancelStream()
	lrs.cancelStream = nil
	lrs.logger.Infof("Stopping LRS stream")
	<-lrs.doneCh
}
