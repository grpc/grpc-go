/*
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
 */

// Package lrs implements load reporting service for xds balancer.
package lrs

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/xds/internal"
	basepb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/core/base"
	loadreportpb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/endpoint/load_report"
	lrsgrpc "google.golang.org/grpc/balancer/xds/internal/proto/envoy/service/load_stats/v2/lrs"
	lrspb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/service/load_stats/v2/lrs"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/backoff"
)

// Store defines the interface for a load store. It keeps loads and can report
// them to a server when requested.
type Store interface {
	CallDropped(category string)
	ReportTo(ctx context.Context, cc *grpc.ClientConn)
}

// lrsStore collects loads from xds balancer, and periodically sends load to the
// server.
type lrsStore struct {
	serviceName  string
	node         *basepb.Node
	backoff      backoff.Strategy
	lastReported time.Time

	drops sync.Map // map[string]*uint64
}

// NewStore creates a store for load reports.
func NewStore(serviceName string) Store {
	return &lrsStore{
		serviceName: serviceName,
		node: &basepb.Node{
			Metadata: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					internal.GrpcHostname: {
						Kind: &structpb.Value_StringValue{StringValue: serviceName},
					},
				},
			},
		},
		backoff: backoff.Exponential{
			MaxDelay: 120 * time.Second,
		},
		lastReported: time.Now(),
	}
}

// Update functions are called by picker for each RPC. To avoid contention, all
// updates are done atomically.

// CallDropped adds one drop record with the given category to store.
func (ls *lrsStore) CallDropped(category string) {
	p, ok := ls.drops.Load(category)
	if !ok {
		tp := new(uint64)
		p, _ = ls.drops.LoadOrStore(category, tp)
	}
	atomic.AddUint64(p.(*uint64), 1)
}

// TODO: add query counts
//  callStarted(l locality)
//  callFinished(l locality, err error)

func (ls *lrsStore) buildStats() []*loadreportpb.ClusterStats {
	var (
		totalDropped uint64
		droppedReqs  []*loadreportpb.ClusterStats_DroppedRequests
	)
	ls.drops.Range(func(category, countP interface{}) bool {
		tempCount := atomic.SwapUint64(countP.(*uint64), 0)
		if tempCount <= 0 {
			return true
		}
		totalDropped += tempCount
		droppedReqs = append(droppedReqs, &loadreportpb.ClusterStats_DroppedRequests{
			Category:     category.(string),
			DroppedCount: tempCount,
		})
		return true
	})

	dur := time.Since(ls.lastReported)
	ls.lastReported = time.Now()

	var ret []*loadreportpb.ClusterStats
	ret = append(ret, &loadreportpb.ClusterStats{
		ClusterName:           ls.serviceName,
		UpstreamLocalityStats: nil, // TODO: populate this to support per locality loads.

		TotalDroppedRequests: totalDropped,
		DroppedRequests:      droppedReqs,
		LoadReportInterval:   ptypes.DurationProto(dur),
	})

	return ret
}

// ReportTo makes a streaming lrs call to cc and blocks.
//
// It retries the call (with backoff) until ctx is canceled.
func (ls *lrsStore) ReportTo(ctx context.Context, cc *grpc.ClientConn) {
	c := lrsgrpc.NewLoadReportingServiceClient(cc)
	var (
		retryCount int
		doBackoff  bool
	)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if doBackoff {
			backoffTimer := time.NewTimer(ls.backoff.Backoff(retryCount))
			select {
			case <-backoffTimer.C:
			case <-ctx.Done():
				backoffTimer.Stop()
				return
			}
			retryCount++
		}

		doBackoff = true
		stream, err := c.StreamLoadStats(ctx)
		if err != nil {
			grpclog.Infof("lrs: failed to create stream: %v", err)
			continue
		}
		if err := stream.Send(&lrspb.LoadStatsRequest{
			Node: ls.node,
		}); err != nil {
			grpclog.Infof("lrs: failed to send first request: %v", err)
			continue
		}
		first, err := stream.Recv()
		if err != nil {
			grpclog.Infof("lrs: failed to receive first response: %v", err)
			continue
		}
		interval, err := ptypes.Duration(first.LoadReportingInterval)
		if err != nil {
			grpclog.Infof("lrs: failed to convert report interval: %v", err)
			continue
		}
		if len(first.Clusters) != 1 || first.Clusters[0] != ls.serviceName {
			grpclog.Infof("lrs: received clusters %v, expect one cluster %q", first.Clusters, ls.serviceName)
			continue
		}
		if first.ReportEndpointGranularity {
			// TODO: fixme to support per endpoint loads.
			grpclog.Infof("lrs: endpoint loads requested, but not supported by current implementation")
			continue
		}

		// No backoff afterwards.
		doBackoff = false
		retryCount = 0
		ls.sendLoads(ctx, stream, first.Clusters[0], interval)
	}
}

func (ls *lrsStore) sendLoads(ctx context.Context, stream lrsgrpc.LoadReportingService_StreamLoadStatsClient, clusterName string, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}
		if err := stream.Send(&lrspb.LoadStatsRequest{
			Node:         ls.node,
			ClusterStats: ls.buildStats(),
		}); err != nil {
			grpclog.Infof("lrs: failed to send report: %v", err)
			return
		}
	}
}
