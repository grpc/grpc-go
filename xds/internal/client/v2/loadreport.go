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

package v2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v2endpointpb "github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	lrsgrpc "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v2"
	lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v2"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc"
	"google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

type lrsStream lrsgrpc.LoadReportingService_StreamLoadStatsClient

func (v2c *client) NewLoadStatsStream(ctx context.Context, cc *grpc.ClientConn) (grpc.ClientStream, error) {
	c := lrsgrpc.NewLoadReportingServiceClient(cc)
	return c.StreamLoadStats(ctx)
}

func (v2c *client) SendFirstLoadStatsRequest(s grpc.ClientStream, targetName string) error {
	stream, ok := s.(lrsStream)
	if !ok {
		return fmt.Errorf("lrs: Attempt to send request on unsupported stream type: %T", s)
	}
	node := proto.Clone(v2c.nodeProto).(*v2corepb.Node)
	if node == nil {
		node = &v2corepb.Node{}
	}
	if node.Metadata == nil {
		node.Metadata = &structpb.Struct{}
	}
	if node.Metadata.Fields == nil {
		node.Metadata.Fields = make(map[string]*structpb.Value)
	}
	node.Metadata.Fields[xdsclient.NodeMetadataHostnameKey] = &structpb.Value{
		Kind: &structpb.Value_StringValue{StringValue: targetName},
	}

	req := &lrspb.LoadStatsRequest{Node: node}
	v2c.logger.Infof("lrs: sending init LoadStatsRequest: %v", req)
	return stream.Send(req)
}

func (v2c *client) HandleLoadStatsResponse(s grpc.ClientStream, clusterName string) (time.Duration, error) {
	stream, ok := s.(lrsStream)
	if !ok {
		return 0, fmt.Errorf("lrs: Attempt to receive response on unsupported stream type: %T", s)
	}

	resp, err := stream.Recv()
	if err != nil {
		return 0, fmt.Errorf("lrs: failed to receive first response: %v", err)
	}
	v2c.logger.Infof("lrs: received first LoadStatsResponse: %+v", resp)

	interval, err := ptypes.Duration(resp.GetLoadReportingInterval())
	if err != nil {
		return 0, fmt.Errorf("lrs: failed to convert report interval: %v", err)
	}

	// The LRS client should join the clusters it knows with the cluster
	// list from response, and send loads for them.
	//
	// But the LRS client now only supports one cluster. TODO: extend it to
	// support multiple clusters.
	var clusterFoundInResponse bool
	for _, c := range resp.Clusters {
		if c == clusterName {
			clusterFoundInResponse = true
		}
	}
	if !clusterFoundInResponse {
		return 0, fmt.Errorf("lrs: received clusters %v does not contain expected {%v}", resp.Clusters, clusterName)
	}
	if resp.ReportEndpointGranularity {
		// TODO: fixme to support per endpoint loads.
		return 0, errors.New("lrs: endpoint loads requested, but not supported by current implementation")
	}

	return interval, nil
}

func (v2c *client) SendLoadStatsRequest(s grpc.ClientStream, clusterName string) error {
	stream, ok := s.(lrsStream)
	if !ok {
		return fmt.Errorf("lrs: Attempt to send request on unsupported stream type: %T", s)
	}
	if v2c.loadStore == nil {
		return errors.New("lrs: LoadStore is not initialized")
	}

	var clusterStats []*v2endpointpb.ClusterStats
	sds := v2c.loadStore.Stats([]string{clusterName})
	for _, sd := range sds {
		var (
			droppedReqs   []*v2endpointpb.ClusterStats_DroppedRequests
			localityStats []*v2endpointpb.UpstreamLocalityStats
		)
		for category, count := range sd.Drops {
			droppedReqs = append(droppedReqs, &v2endpointpb.ClusterStats_DroppedRequests{
				Category:     category,
				DroppedCount: count,
			})
		}
		for l, localityData := range sd.LocalityStats {
			lid, err := internal.LocalityIDFromString(l)
			if err != nil {
				return err
			}
			var loadMetricStats []*v2endpointpb.EndpointLoadMetricStats
			for name, loadData := range localityData.LoadStats {
				loadMetricStats = append(loadMetricStats, &v2endpointpb.EndpointLoadMetricStats{
					MetricName:                    name,
					NumRequestsFinishedWithMetric: loadData.Count,
					TotalMetricValue:              loadData.Sum,
				})
			}
			localityStats = append(localityStats, &v2endpointpb.UpstreamLocalityStats{
				Locality: &v2corepb.Locality{
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

		clusterStats = append(clusterStats, &v2endpointpb.ClusterStats{
			ClusterName:           sd.Cluster,
			ClusterServiceName:    sd.Service,
			UpstreamLocalityStats: localityStats,
			TotalDroppedRequests:  sd.TotalDrops,
			DroppedRequests:       droppedReqs,
			LoadReportInterval:    ptypes.DurationProto(sd.ReportInterval),
		})

	}

	req := &lrspb.LoadStatsRequest{ClusterStats: clusterStats}
	v2c.logger.Infof("lrs: sending LRS loads: %+v", req)
	return stream.Send(req)
}
