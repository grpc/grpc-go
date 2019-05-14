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
 */

package lrs

import (
	"context"
	"io"
	"net"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	durationpb "github.com/golang/protobuf/ptypes/duration"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer/xds/internal"
	basepb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/core/base"
	loadreportpb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/api/v2/endpoint/load_report"
	lrsgrpc "google.golang.org/grpc/balancer/xds/internal/proto/envoy/service/load_stats/v2/lrs"
	lrspb "google.golang.org/grpc/balancer/xds/internal/proto/envoy/service/load_stats/v2/lrs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testService = "grpc.service.test"

var dropCategories = []string{"drop_for_real", "drop_for_fun"}

// equalClusterStats sorts requests and clear report internal before comparing.
func equalClusterStats(a, b []*loadreportpb.ClusterStats) bool {
	for _, s := range a {
		sort.Slice(s.DroppedRequests, func(i, j int) bool {
			return s.DroppedRequests[i].Category < s.DroppedRequests[j].Category
		})
		s.LoadReportInterval = nil
	}
	for _, s := range b {
		sort.Slice(s.DroppedRequests, func(i, j int) bool {
			return s.DroppedRequests[i].Category < s.DroppedRequests[j].Category
		})
		s.LoadReportInterval = nil
	}
	return reflect.DeepEqual(a, b)
}

func Test_lrsStore_buildStats(t *testing.T) {
	tests := []struct {
		name  string
		drops []map[string]uint64
	}{
		{
			name: "one report",
			drops: []map[string]uint64{{
				dropCategories[0]: 31,
				dropCategories[1]: 41,
			}},
		},
		{
			name: "two reports",
			drops: []map[string]uint64{{
				dropCategories[0]: 31,
				dropCategories[1]: 41,
			}, {
				dropCategories[0]: 59,
				dropCategories[1]: 26,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ls := NewStore(testService).(*lrsStore)

			for _, ds := range tt.drops {
				var (
					totalDropped uint64
					droppedReqs  []*loadreportpb.ClusterStats_DroppedRequests
				)
				for cat, count := range ds {
					totalDropped += count
					droppedReqs = append(droppedReqs, &loadreportpb.ClusterStats_DroppedRequests{
						Category:     cat,
						DroppedCount: count,
					})
				}
				want := []*loadreportpb.ClusterStats{
					{
						ClusterName:          testService,
						TotalDroppedRequests: totalDropped,
						DroppedRequests:      droppedReqs,
					},
				}

				var wg sync.WaitGroup
				for c, count := range ds {
					for i := 0; i < int(count); i++ {
						wg.Add(1)
						go func(i int, c string) {
							ls.CallDropped(c)
							wg.Done()
						}(i, c)
					}
				}
				wg.Wait()

				if got := ls.buildStats(); !equalClusterStats(got, want) {
					t.Errorf("lrsStore.buildStats() = %v, want %v", got, want)
					t.Errorf("%s", cmp.Diff(got, want))
				}
			}
		})
	}
}

type lrsServer struct {
	mu                sync.Mutex
	dropTotal         uint64
	drops             map[string]uint64
	reportingInterval *durationpb.Duration
}

func (lrss *lrsServer) StreamLoadStats(stream lrsgrpc.LoadReportingService_StreamLoadStatsServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if !proto.Equal(req, &lrspb.LoadStatsRequest{
		Node: &basepb.Node{
			Metadata: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					internal.GrpcHostname: {
						Kind: &structpb.Value_StringValue{StringValue: testService},
					},
				},
			},
		},
	}) {
		return status.Errorf(codes.FailedPrecondition, "unexpected req: %+v", req)
	}
	if err := stream.Send(&lrspb.LoadStatsResponse{
		Clusters:              []string{testService},
		LoadReportingInterval: lrss.reportingInterval,
	}); err != nil {
		return err
	}

	for {
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		stats := req.ClusterStats[0]
		lrss.mu.Lock()
		lrss.dropTotal += stats.TotalDroppedRequests
		for _, d := range stats.DroppedRequests {
			lrss.drops[d.Category] += d.DroppedCount
		}
		lrss.mu.Unlock()
	}
}

func setupServer(t *testing.T, reportingInterval *durationpb.Duration) (addr string, lrss *lrsServer, cleanup func()) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen failed due to: %v", err)
	}
	svr := grpc.NewServer()
	lrss = &lrsServer{
		drops:             make(map[string]uint64),
		reportingInterval: reportingInterval,
	}
	lrsgrpc.RegisterLoadReportingServiceServer(svr, lrss)
	go svr.Serve(lis)
	return lis.Addr().String(), lrss, func() {
		svr.Stop()
		lis.Close()
	}
}

func Test_lrsStore_ReportTo(t *testing.T) {
	const intervalNano = 1000 * 1000 * 50
	addr, lrss, cleanup := setupServer(t, &durationpb.Duration{
		Seconds: 0,
		Nanos:   intervalNano,
	})
	defer cleanup()

	ls := NewStore(testService)
	cc, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		ls.ReportTo(ctx, cc)
		close(done)
	}()

	drops := map[string]uint64{
		dropCategories[0]: 31,
		dropCategories[1]: 41,
	}

	for c, d := range drops {
		for i := 0; i < int(d); i++ {
			ls.CallDropped(c)
			time.Sleep(time.Nanosecond * intervalNano / 10)
		}
	}
	time.Sleep(time.Nanosecond * intervalNano * 2)
	cancel()
	<-done

	lrss.mu.Lock()
	defer lrss.mu.Unlock()
	if !cmp.Equal(lrss.drops, drops) {
		t.Errorf("different: %v", cmp.Diff(lrss.drops, drops))
	}
}
