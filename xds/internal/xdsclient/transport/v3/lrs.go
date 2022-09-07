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

package v3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal"
	"google.golang.org/grpc/xds/internal/xdsclient/load"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3lrsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

const clientFeatureLRSSendAllClusters = "envoy.lrs.supports_send_all_clusters"

type lrsStream v3lrsgrpc.LoadReportingService_StreamLoadStatsClient

// NewLoadStatsStream returns a new LRS client stream.
func (t *Transport) NewLoadStatsStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error) {
	return v3lrsgrpc.NewLoadReportingServiceClient(cc).StreamLoadStats(ctx)
}

// SendFirstLoadStatsRequest constructs and sends the first LoadStatsRequest
// message.
func (t *Transport) SendFirstLoadStatsRequest(s grpc.ClientStream) error {
	stream, ok := s.(lrsStream)
	if !ok {
		return fmt.Errorf("unsupported stream type: %T", s)
	}

	node := proto.Clone(t.nodeProto).(*v3corepb.Node)
	node.ClientFeatures = append(node.ClientFeatures, clientFeatureLRSSendAllClusters)
	req := &v3lrspb.LoadStatsRequest{Node: node}
	t.logger.Debugf("Sending initial LoadStatsRequest: %s", pretty.ToJSON(req))
	err := stream.Send(req)
	if err == io.EOF {
		return getStreamError(stream)
	}
	return err
}

// RecvFirstLoadStatsResponse reads the first LoadStatsResponse message.
func (t *Transport) RecvFirstLoadStatsResponse(s grpc.ClientStream) ([]string, time.Duration, error) {
	stream, ok := s.(lrsStream)
	if !ok {
		return nil, 0, fmt.Errorf("unsupported stream type: %T", s)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to receive first LoadStatsResponse: %v", err)
	}
	t.logger.Debugf("Received first LoadStatsResponse: %s", pretty.ToJSON(resp))

	interval, err := ptypes.Duration(resp.GetLoadReportingInterval())
	if err != nil {
		return nil, 0, fmt.Errorf("invalid load_reporting_interval: %v", err)
	}

	if resp.ReportEndpointGranularity {
		// TODO: fixme to support per endpoint loads.
		return nil, 0, errors.New("lrs: endpoint loads requested, but not supported by current implementation")
	}

	clusters := resp.Clusters
	if resp.SendAllClusters {
		// Return nil to send stats for all clusters.
		clusters = nil
	}

	return clusters, interval, nil
}

// TODO(easwars): Move load.Data struct to some place else, so that this package
// does not have a dependency "xds/internal/xdsclient/load".

// SendLoadStatsRequest constructs and sends a LoadStatsRequest message.
func (t *Transport) SendLoadStatsRequest(s grpc.ClientStream, loads []*load.Data) error {
	stream, ok := s.(lrsStream)
	if !ok {
		return fmt.Errorf("unsupported stream type: %T", s)
	}

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
			LoadReportInterval:    ptypes.DurationProto(sd.ReportInterval),
		})
	}

	req := &v3lrspb.LoadStatsRequest{ClusterStats: clusterStats}
	t.logger.Debugf("Sending LRS loads: %s", pretty.ToJSON(req))
	err := stream.Send(req)
	if err == io.EOF {
		return getStreamError(stream)
	}
	return err
}

func getStreamError(stream lrsStream) error {
	for {
		if _, err := stream.Recv(); err != nil {
			return err
		}
	}
}
