/*
 *
 * Copyright 2025 gRPC authors.
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

package lrsclient

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/envconfig"
	igrpclog "google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/internal/xds/clients"
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

// streamImpl provides all the functionality associated with an LRS (Load
// Reporting Service) stream on the client-side. It manages the lifecycle of
// the LRS stream, including starting, stopping, and retrying the stream. It
// also provides a LoadStore that can be used to report load, with a Stop
// function that should be called when the load reporting is no longer
// needed.
type streamImpl struct {
	// The following fields are initialized when a stream instance is created
	// and are read-only afterwards, and hence can be accessed without a mutex.
	transport   clients.Transport       // Transport to use for LRS stream.
	backoff     func(int) time.Duration // Backoff for retries, after stream failures.
	minInterval time.Duration           // Lower bound on the load reporting interval.
	nodeProto   *v3corepb.Node          // Identifies the gRPC application.
	doneCh      chan struct{}           // To notify exit of LRS goroutine.
	logger      *igrpclog.PrefixLogger

	cancelStream context.CancelFunc // Cancel the stream. If nil, the stream is not active.
	loadStore    *LoadStore         // LoadStore returned to user for pushing loads.

	finalSendRequest chan struct{} // To request for the final attempt to send loads.
	finalSendDone    chan error    // To signal completion of the final attempt of sending loads.
}

// streamOpts holds the options for creating an lrsStream.
type streamOpts struct {
	transport   clients.Transport       // xDS transport to create the stream on.
	backoff     func(int) time.Duration // Backoff for retries, after stream failures.
	minInterval time.Duration           // Lower bound on the load reporting interval.
	nodeProto   *v3corepb.Node          // Node proto to identify the gRPC application.
	logPrefix   string                  // Prefix to be used for log messages.
}

// newStreamImpl creates a new StreamImpl with the provided options.
//
// The actual streaming RPC call is initiated when the first call to ReportLoad
// is made, and is terminated when the last call to ReportLoad is canceled.
func newStreamImpl(opts streamOpts) *streamImpl {
	ctx, cancel := context.WithCancel(context.Background())

	lrs := &streamImpl{
		transport:        opts.transport,
		backoff:          opts.backoff,
		minInterval:      opts.minInterval,
		nodeProto:        opts.nodeProto,
		cancelStream:     cancel,
		doneCh:           make(chan struct{}),
		finalSendRequest: make(chan struct{}, 1),
		finalSendDone:    make(chan error, 1),
	}

	l := grpclog.Component("xds")
	lrs.logger = igrpclog.NewPrefixLogger(l, opts.logPrefix+fmt.Sprintf("[lrs-stream %p] ", lrs))
	lrs.loadStore = newLoadStore()
	go lrs.runner(ctx)
	return lrs
}

// runner is responsible for managing the lifetime of an LRS streaming call. It
// creates the stream, sends the initial LoadStatsRequest, starts a goroutine to
// read LoadStatsResponses, and periodically sends LoadStatsRequests. The
// runner will restart the stream if it encounters any errors.
func (lrs *streamImpl) runner(ctx context.Context) {
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

		stream, err := lrs.transport.NewStream(streamCtx, "/envoy.service.load_stats.v3.LoadReportingService/StreamLoadStats")
		if err != nil {
			lrs.logger.Warningf("Failed to create new LRS streaming RPC: %v", err)
			return nil
		}
		if lrs.logger.V(2) {
			lrs.logger.Infof("LRS stream created")
		}

		if err := lrs.sendFirstLoadStatsRequest(stream, node); err != nil {
			lrs.logger.Warningf("Sending first LRS request failed: %v", err)
			return nil
		}

		// Start a goroutine that reads every response on this stream, including
		// the first one, and passes them to us on updateCh.
		updateCh := make(chan lrsUpdate)
		readerDone := make(chan struct{})
		go func() {
			defer close(readerDone)
			lrs.recvLoadStatsResponses(streamCtx, stream, updateCh)
		}()
		// Make sure the reader goroutine has exited before we return, so that
		// it never outlives this stream. Canceling streamCtx unblocks any
		// pending stream.Recv() call.
		defer func() {
			cancel()
			<-readerDone
		}()

		// Wait for the first valid response. The reader drops invalid
		// responses, so we keep waiting until we get a valid one or the
		// stream fails.
		var first lrsUpdate
		select {
		case first = <-updateCh:
		case <-streamCtx.Done():
			return nil
		}
		if first.err != nil {
			return nil
		}

		// We reset backoff state when we successfully receive at least one
		// message from the server.
		lrs.sendLoads(streamCtx, stream, first.clusters, first.interval, updateCh)
		return backoff.ErrResetBackoff
	}
	backoff.RunF(ctx, runLoadReportStream, lrs.backoff)
}

// lrsUpdate is sent by the reader goroutine to the goroutine sending loads.
// Exactly one of the following is true:
//   - err is non-nil: the stream has failed, and no more updates will be sent.
//   - err is nil: clusters and interval contain the config from a valid
//     LoadStatsResponse.
type lrsUpdate struct {
	clusters []string
	interval time.Duration
	err      error
}

// sendLoads is responsible for periodically sending load reports to the LRS
// server at the specified interval for the specified clusters. It applies new
// config received on updateCh, and returns when the passed in context is
// canceled, when the reader goroutine reports a stream failure, or after the
// final load report is sent.
func (lrs *streamImpl) sendLoads(ctx context.Context, stream clients.Stream, clusterNames []string, interval time.Duration, updateCh <-chan lrsUpdate) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case u := <-updateCh:
			if u.err != nil {
				// The reader goroutine has already logged the error.
				return
			}
			clusterNames = u.clusters
			// Only reset the ticker if the interval changed. Otherwise, a
			// server that keeps sending the same response would keep pushing
			// out the next load report.
			if u.interval != interval {
				interval = u.interval
				tick.Reset(interval)
			}
			continue
		case <-ctx.Done():
			return
		case <-lrs.finalSendRequest:
			var finalSendErr error
			if lrs.logger.V(2) {
				lrs.logger.Infof("Final send request received. Attempting final LRS report.")
			}
			if err := lrs.sendLoadStatsRequest(stream, lrs.loadStore.stats(clusterNames)); err != nil {
				lrs.logger.Warningf("Failed to send final load report. Writing to LRS stream failed: %v", err)
				finalSendErr = err
			}
			if lrs.logger.V(2) {
				lrs.logger.Infof("Successfully sent final load report.")
			}
			lrs.finalSendDone <- finalSendErr
			return
		}

		if err := lrs.sendLoadStatsRequest(stream, lrs.loadStore.stats(clusterNames)); err != nil {
			lrs.logger.Warningf("Failed to send periodic load report. Writing to LRS stream failed: %v", err)
			return
		}
	}
}

func (lrs *streamImpl) sendFirstLoadStatsRequest(stream clients.Stream, node *v3corepb.Node) error {
	req := &v3lrspb.LoadStatsRequest{Node: node}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Sending initial LoadStatsRequest: %s", pretty.ToJSON(req))
	}
	msg, err := proto.Marshal(req)
	if err != nil {
		lrs.logger.Warningf("Failed to marshal LoadStatsRequest: %v", err)
		return err
	}
	err = stream.Send(msg)
	if err == io.EOF {
		return getStreamError(stream)
	}
	return err
}

// recvLoadStatsResponses reads LoadStatsResponse messages from the stream and
// sends them to updateCh, until reading from the stream fails or ctx is
// canceled.
//
// Responses that fail to parse are logged and dropped, and the existing config
// stays in effect. This matches what grpc-java and grpc C++ do. Only a failure
// to read from the stream ends the stream.
//
// This is the only goroutine that calls stream.Recv() once the first request
// has been sent.
func (lrs *streamImpl) recvLoadStatsResponses(ctx context.Context, stream clients.Stream, updateCh chan<- lrsUpdate) {
	for {
		r, err := stream.Recv()
		if err != nil {
			if ctx.Err() == nil {
				lrs.logger.Warningf("Reading from LRS streaming RPC failed: %v", err)
			}
			select {
			case updateCh <- lrsUpdate{err: err}:
			case <-ctx.Done():
			}
			return
		}

		clusters, interval, err := lrs.parseLoadStatsResponse(r)
		if err != nil {
			lrs.logger.Warningf("Ignoring invalid LoadStatsResponse: %v", err)
			continue
		}

		select {
		case updateCh <- lrsUpdate{clusters: clusters, interval: interval}:
		case <-ctx.Done():
			return
		}
	}
}

// parseLoadStatsResponse parses a LoadStatsResponse received from the LRS
// server. Returns the following:
//   - a list of cluster names requested by the server or an empty slice if the
//     server requested for load from all clusters
//   - the load reporting interval, and
//   - any error encountered
func (lrs *streamImpl) parseLoadStatsResponse(r []byte) ([]string, time.Duration, error) {
	var resp v3lrspb.LoadStatsResponse
	if err := proto.Unmarshal(r, &resp); err != nil {
		return nil, 0, fmt.Errorf("lrs: failed to unmarshal LoadStatsResponse: %v", err)
	}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Received LoadStatsResponse: %s", pretty.ToJSON(&resp))
	}

	interval := resp.GetLoadReportingInterval()
	if err := interval.CheckValid(); err != nil && lrs.logger.V(2) {
		// Unset and invalid intervals are not rejected. AsDuration converts
		// them to the closest time.Duration (zero for unset), which is then
		// clamped to the configured minimum below if required.
		lrs.logger.Warningf("Received invalid load_reporting_interval: %v", err)
	}
	loadReportingInterval := interval.AsDuration()
	if loadReportingInterval < lrs.minInterval {
		// Clamp the interval to the configured minimum. This also handles zero
		// and negative intervals, which are valid Durations but cannot be used
		// as the period of a ticker.
		lrs.logger.Warningf("Received load_reporting_interval %v is less than the minimum, using %v instead", loadReportingInterval, lrs.minInterval)
		loadReportingInterval = lrs.minInterval
	}

	clusters := resp.Clusters
	if resp.SendAllClusters {
		// Return an empty slice to send stats for all clusters.
		clusters = []string{}
	}
	return clusters, loadReportingInterval, nil
}

func (lrs *streamImpl) sendLoadStatsRequest(stream clients.Stream, loads []*loadData) error {
	clusterStats := make([]*v3endpointpb.ClusterStats, 0, len(loads))
	for _, sd := range loads {
		droppedReqs := make([]*v3endpointpb.ClusterStats_DroppedRequests, 0, len(sd.drops))
		for category, count := range sd.drops {
			droppedReqs = append(droppedReqs, &v3endpointpb.ClusterStats_DroppedRequests{
				Category:     category,
				DroppedCount: count,
			})
		}
		localityStats := make([]*v3endpointpb.UpstreamLocalityStats, 0, len(sd.localityStats))
		for lid, localityData := range sd.localityStats {
			loadMetricStats := make([]*v3endpointpb.EndpointLoadMetricStats, 0, len(localityData.loadStats))
			var cpuUtilization, memUtilization, appUtilization *v3endpointpb.UnnamedEndpointLoadMetricStats
			for name, loadData := range localityData.loadStats {
				if envconfig.XDSORCAToLRSPropEnabled {
					switch name {
					case "cpu_utilization":
						cpuUtilization = &v3endpointpb.UnnamedEndpointLoadMetricStats{
							NumRequestsFinishedWithMetric: loadData.count,
							TotalMetricValue:              loadData.sum,
						}
					case "mem_utilization":
						memUtilization = &v3endpointpb.UnnamedEndpointLoadMetricStats{
							NumRequestsFinishedWithMetric: loadData.count,
							TotalMetricValue:              loadData.sum,
						}
					case "application_utilization":
						appUtilization = &v3endpointpb.UnnamedEndpointLoadMetricStats{
							NumRequestsFinishedWithMetric: loadData.count,
							TotalMetricValue:              loadData.sum,
						}
					default:
						loadMetricStats = append(loadMetricStats, &v3endpointpb.EndpointLoadMetricStats{
							MetricName:                    name,
							NumRequestsFinishedWithMetric: loadData.count,
							TotalMetricValue:              loadData.sum,
						})
					}
				} else {
					loadMetricStats = append(loadMetricStats, &v3endpointpb.EndpointLoadMetricStats{
						MetricName:                    name,
						NumRequestsFinishedWithMetric: loadData.count,
						TotalMetricValue:              loadData.sum,
					})
				}
			}
			localityStats = append(localityStats, &v3endpointpb.UpstreamLocalityStats{
				Locality: &v3corepb.Locality{
					Region:  lid.Region,
					Zone:    lid.Zone,
					SubZone: lid.SubZone,
				},
				TotalSuccessfulRequests: localityData.requestStats.succeeded,
				TotalRequestsInProgress: localityData.requestStats.inProgress,
				TotalErrorRequests:      localityData.requestStats.errored,
				TotalIssuedRequests:     localityData.requestStats.issued,
				CpuUtilization:          cpuUtilization,
				MemUtilization:          memUtilization,
				ApplicationUtilization:  appUtilization,
				LoadMetricStats:         loadMetricStats,
				UpstreamEndpointStats:   nil, // TODO: populate for per endpoint loads.
			})
		}

		clusterStats = append(clusterStats, &v3endpointpb.ClusterStats{
			ClusterName:           sd.cluster,
			ClusterServiceName:    sd.service,
			UpstreamLocalityStats: localityStats,
			TotalDroppedRequests:  sd.totalDrops,
			DroppedRequests:       droppedReqs,
			LoadReportInterval:    durationpb.New(sd.reportInterval),
		})
	}

	req := &v3lrspb.LoadStatsRequest{ClusterStats: clusterStats}
	if lrs.logger.V(perRPCVerbosityLevel) {
		lrs.logger.Infof("Sending LRS loads: %s", pretty.ToJSON(req))
	}
	msg, err := proto.Marshal(req)
	if err != nil {
		if lrs.logger.V(2) {
			lrs.logger.Infof("Failed to marshal LoadStatsRequest: %v", err)
		}
		return err
	}
	return stream.Send(msg)
}

func getStreamError(stream clients.Stream) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return err
		}
	}
}
