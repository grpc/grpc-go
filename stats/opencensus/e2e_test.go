/*
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
 */

package opencensus

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/leakcheck"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func init() {
	// OpenCensus, once included in binary, will spawn a global goroutine
	// recorder that is not controllable by application.
	// https://github.com/census-instrumentation/opencensus-go/issues/1191
	leakcheck.RegisterIgnoreGoroutine("go.opencensus.io/stats/view.(*worker).start")
}

var defaultTestTimeout = 5 * time.Second

type fakeExporter struct {
	t *testing.T

	mu        sync.RWMutex
	seenViews map[string]*viewInformation
	seenSpans []spanInformation
}

// viewInformation is information Exported from the view package through
// ExportView relevant to testing, i.e. a reasonably non flaky expectation of
// desired emissions to Exporter.
type viewInformation struct {
	aggType    view.AggType
	aggBuckets []float64
	desc       string
	tagKeys    []tag.Key
	rows       []*view.Row
}

func (fe *fakeExporter) ExportView(vd *view.Data) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.seenViews[vd.View.Name] = &viewInformation{
		aggType:    vd.View.Aggregation.Type,
		aggBuckets: vd.View.Aggregation.Buckets,
		desc:       vd.View.Description,
		tagKeys:    vd.View.TagKeys,
		rows:       vd.Rows,
	}
}

// compareRows compares rows with respect to the information desired to test.
// Both the tags representing the rows and also the data of the row are tested
// for equality. Rows are in nondeterministic order when ExportView is called,
// but handled inside this function by sorting.
func compareRows(rows []*view.Row, rows2 []*view.Row) bool {
	if len(rows) != len(rows2) {
		return false
	}
	// Sort both rows according to the same rule. This is to take away non
	// determinism in the row ordering passed to the Exporter, while keeping the
	// row data.
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].String() > rows[j].String()
	})

	sort.Slice(rows2, func(i, j int) bool {
		return rows2[i].String() > rows2[j].String()
	})

	for i, row := range rows {
		if !cmp.Equal(row.Tags, rows2[i].Tags, cmp.Comparer(func(a tag.Key, b tag.Key) bool {
			return a.Name() == b.Name()
		})) {
			return false
		}
		if !compareData(row.Data, rows2[i].Data) {
			return false
		}
	}
	return true
}

// compareData returns whether the two aggregation data's are equal to each
// other with respect to parts of the data desired for correct emission. The
// function first makes sure the two types of aggregation data are the same, and
// then checks the equality for the respective aggregation data type.
func compareData(ad view.AggregationData, ad2 view.AggregationData) bool {
	if ad == nil && ad2 == nil {
		return true
	}
	if ad == nil || ad2 == nil {
		return false
	}
	if reflect.TypeOf(ad) != reflect.TypeOf(ad2) {
		return false
	}
	switch ad1 := ad.(type) {
	case *view.DistributionData:
		dd2 := ad2.(*view.DistributionData)
		// Count and Count Per Buckets are reasonable for correctness,
		// especially since we verify equality of bucket endpoints elsewhere.
		if ad1.Count != dd2.Count {
			return false
		}
		for i, count := range ad1.CountPerBucket {
			if count != dd2.CountPerBucket[i] {
				return false
			}
		}
	case *view.CountData:
		cd2 := ad2.(*view.CountData)
		return ad1.Value == cd2.Value

		// gRPC open census plugin does not have these next two types of aggregation
		// data types present, for now just check for type equality between the two
		// aggregation data points (done above).
		// case *view.SumData
		// case *view.LastValueData:
	}
	return true
}

func (vi *viewInformation) Equal(vi2 *viewInformation) bool {
	if vi == nil && vi2 == nil {
		return true
	}
	if vi == nil || vi2 == nil {
		return false
	}
	if vi.aggType != vi2.aggType {
		return false
	}
	if !cmp.Equal(vi.aggBuckets, vi2.aggBuckets) {
		return false
	}
	if vi.desc != vi2.desc {
		return false
	}
	if !cmp.Equal(vi.tagKeys, vi2.tagKeys, cmp.Comparer(func(a tag.Key, b tag.Key) bool {
		return a.Name() == b.Name()
	})) {
		return false
	}
	if !compareRows(vi.rows, vi2.rows) {
		return false
	}
	return true
}

// distributionDataLatencyCount checks if the view information contains the
// desired distribution latency total count that falls in buckets of 5 seconds or
// less. This must be called with non nil view information that is aggregated
// with distribution data. Returns a nil error if correct count information
// found, non nil error if correct information not found.
func distributionDataLatencyCount(vi *viewInformation, countWant int64, wantTags [][]tag.Tag) error {
	var totalCount int64
	var largestIndexWithFive int
	for i, bucket := range vi.aggBuckets {
		// Distribution for latency is measured in milliseconds, so 5 * 1000 =
		// 5000.
		if bucket > 5000 {
			largestIndexWithFive = i
			break
		}
	}
	// Sort rows by string name. This is to take away non determinism in the row
	// ordering passed to the Exporter, while keeping the row data.
	sort.Slice(vi.rows, func(i, j int) bool {
		return vi.rows[i].String() > vi.rows[j].String()
	})
	// Iterating through rows sums up data points for all methods. In this case,
	// a data point for the unary and for the streaming RPC.
	for i, row := range vi.rows {
		// The method names corresponding to unary and streaming call should
		// have the leading slash removed.
		if diff := cmp.Diff(row.Tags, wantTags[i], cmp.Comparer(func(a tag.Key, b tag.Key) bool {
			return a.Name() == b.Name()
		})); diff != "" {
			return fmt.Errorf("wrong tag keys for unary method -got, +want: %v", diff)
		}
		// This could potentially have an extra measurement in buckets above 5s,
		// but that's fine. Count of buckets that could contain up to 5s is a
		// good enough assertion.
		for i, count := range row.Data.(*view.DistributionData).CountPerBucket {
			if i >= largestIndexWithFive {
				break
			}
			totalCount = totalCount + count
		}
	}
	if totalCount != countWant {
		return fmt.Errorf("wrong total count for counts under 5: %v, wantCount: %v", totalCount, countWant)
	}
	return nil
}

// waitForServerCompletedRPCs waits until both Unary and Streaming metric rows
// appear, in two separate rows, for server completed RPC's view. Returns an
// error if the Unary and Streaming metric are not found within the passed
// context's timeout.
func waitForServerCompletedRPCs(ctx context.Context) error {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		rows, err := view.RetrieveData("grpc.io/server/completed_rpcs")
		if err != nil {
			continue
		}
		unaryFound := false
		streamingFound := false
		for _, row := range rows {
			for _, tag := range row.Tags {
				if tag.Value == "grpc.testing.TestService/UnaryCall" {
					unaryFound = true
					break
				} else if tag.Value == "grpc.testing.TestService/FullDuplexCall" {
					streamingFound = true
					break
				}
			}
			if unaryFound && streamingFound {
				return nil
			}
		}
	}
	return fmt.Errorf("timeout when waiting for Unary and Streaming rows to be present for \"grpc.io/server/completed_rpcs\"")
}

// TestAllMetricsOneFunction tests emitted metrics from gRPC. It registers all
// the metrics provided by this package. It then configures a system with a gRPC
// Client and gRPC server with the OpenCensus Dial and Server Option configured,
// and makes a Unary RPC and a Streaming RPC. These two RPCs should cause
// certain emissions for each registered metric through the OpenCensus View
// package.
func (s) TestAllMetricsOneFunction(t *testing.T) {
	allViews := []*view.View{
		ClientStartedRPCsView,
		ServerStartedRPCsView,
		ClientCompletedRPCsView,
		ServerCompletedRPCsView,
		ClientSentBytesPerRPCView,
		ClientSentCompressedMessageBytesPerRPCView,
		ServerSentBytesPerRPCView,
		ServerSentCompressedMessageBytesPerRPCView,
		ClientReceivedBytesPerRPCView,
		ClientReceivedCompressedMessageBytesPerRPCView,
		ServerReceivedBytesPerRPCView,
		ServerReceivedCompressedMessageBytesPerRPCView,
		ClientSentMessagesPerRPCView,
		ServerSentMessagesPerRPCView,
		ClientReceivedMessagesPerRPCView,
		ServerReceivedMessagesPerRPCView,
		ClientRoundtripLatencyView,
		ServerLatencyView,
		ClientAPILatencyView,
	}
	view.Register(allViews...)
	// Unregister unconditionally in this defer to correctly cleanup globals in
	// error conditions.
	defer view.Unregister(allViews...)
	fe := &fakeExporter{
		t:         t,
		seenViews: make(map[string]*viewInformation),
	}
	view.RegisterExporter(fe)
	defer view.UnregisterExporter(fe)

	ss := &stubserver.StubServer{
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{Payload: &testpb.Payload{
				Body: make([]byte, 10000),
			}}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	if err := ss.Start([]grpc.ServerOption{ServerOption(TraceOptions{})}, DialOption(TraceOptions{})); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Make two RPC's, a unary RPC and a streaming RPC. These should cause
	// certain metrics to be emitted.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{
		Body: make([]byte, 10000),
	}}, grpc.UseCompressor(gzip.Name)); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %f", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	cmtk := tag.MustNewKey("grpc_client_method")
	smtk := tag.MustNewKey("grpc_server_method")
	cstk := tag.MustNewKey("grpc_client_status")
	sstk := tag.MustNewKey("grpc_server_status")
	wantMetrics := []struct {
		metric   *view.View
		wantVI   *viewInformation
		wantTags [][]tag.Tag // for non deterministic (i.e. latency) metrics. First dimension represents rows.
	}{
		{
			metric: ClientStartedRPCsView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeCount,
				aggBuckets: []float64{},
				desc:       "Number of opened client RPCs, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},

				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
				},
			},
		},
		{
			metric: ServerStartedRPCsView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeCount,
				aggBuckets: []float64{},
				desc:       "Number of opened server RPCs, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
				},
			},
		},
		{
			metric: ClientCompletedRPCsView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeCount,
				aggBuckets: []float64{},
				desc:       "Number of completed RPCs by method and status.",
				tagKeys: []tag.Key{
					cmtk,
					cstk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
							{
								Key:   cstk,
								Value: "OK",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
							{
								Key:   cstk,
								Value: "OK",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
				},
			},
		},
		{
			metric: ServerCompletedRPCsView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeCount,
				aggBuckets: []float64{},
				desc:       "Number of completed RPCs by method and status.",
				tagKeys: []tag.Key{
					smtk,
					sstk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
							{
								Key:   sstk,
								Value: "OK",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
							{
								Key:   sstk,
								Value: "OK",
							},
						},
						Data: &view.CountData{
							Value: 1,
						},
					},
				},
			},
		},
		{
			metric: ClientSentBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of sent bytes per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientSentCompressedMessageBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of sent compressed message bytes per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerSentBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of sent bytes per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerSentCompressedMessageBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of sent compressed message bytes per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientReceivedBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of received bytes per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientReceivedCompressedMessageBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of received compressed message bytes per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerReceivedBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of received bytes per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerReceivedCompressedMessageBytesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: bytesDistributionBounds,
				desc:       "Distribution of received compressed message bytes per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientSentMessagesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: countDistributionBounds,
				desc:       "Distribution of sent messages per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerSentMessagesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: countDistributionBounds,
				desc:       "Distribution of sent messages per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientReceivedMessagesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: countDistributionBounds,
				desc:       "Distribution of received messages per RPC, by method.",
				tagKeys: []tag.Key{
					cmtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   cmtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ServerReceivedMessagesPerRPCView,
			wantVI: &viewInformation{
				aggType:    view.AggTypeDistribution,
				aggBuckets: countDistributionBounds,
				desc:       "Distribution of received messages per RPC, by method.",
				tagKeys: []tag.Key{
					smtk,
				},
				rows: []*view.Row{
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/UnaryCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
					{
						Tags: []tag.Tag{
							{
								Key:   smtk,
								Value: "grpc.testing.TestService/FullDuplexCall",
							},
						},
						Data: &view.DistributionData{
							Count:          1,
							CountPerBucket: []int64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
						},
					},
				},
			},
		},
		{
			metric: ClientRoundtripLatencyView,
			wantTags: [][]tag.Tag{
				{
					{
						Key:   cmtk,
						Value: "grpc.testing.TestService/UnaryCall",
					},
				},
				{
					{
						Key:   cmtk,
						Value: "grpc.testing.TestService/FullDuplexCall",
					},
				},
			},
		},
		{
			metric: ServerLatencyView,
			wantTags: [][]tag.Tag{
				{
					{
						Key:   smtk,
						Value: "grpc.testing.TestService/UnaryCall",
					},
				},
				{
					{
						Key:   smtk,
						Value: "grpc.testing.TestService/FullDuplexCall",
					},
				},
			},
		},
		// Per call metrics:
		{
			metric: ClientAPILatencyView,
			wantTags: [][]tag.Tag{
				{
					{
						Key:   cmtk,
						Value: "grpc.testing.TestService/UnaryCall",
					},
					{
						Key:   cstk,
						Value: "OK",
					},
				},
				{
					{
						Key:   cmtk,
						Value: "grpc.testing.TestService/FullDuplexCall",
					},
					{
						Key:   cstk,
						Value: "OK",
					},
				},
			},
		},
	}
	// Server Side stats.End call happens asynchronously for both Unary and
	// Streaming calls with respect to the RPC returning client side. Thus, add
	// a sync point at the global view package level for these two rows to be
	// recorded, which will be synchronously uploaded to exporters right after.
	if err := waitForServerCompletedRPCs(ctx); err != nil {
		t.Fatal(err)
	}
	view.Unregister(allViews...)
	// Assert the expected emissions for each metric match the expected
	// emissions.
	for _, wantMetric := range wantMetrics {
		metricName := wantMetric.metric.Name
		var vi *viewInformation
		if vi = fe.seenViews[metricName]; vi == nil {
			t.Fatalf("couldn't find %v in the views exported, never collected", metricName)
		}

		// For latency metrics, there is a lot of non determinism about
		// the exact milliseconds of RPCs that finish. Thus, rather than
		// declare the exact data you want, make sure the latency
		// measurement points for the two RPCs above fall within buckets
		// that fall into less than 5 seconds, which is the rpc timeout.
		if metricName == "grpc.io/client/roundtrip_latency" || metricName == "grpc.io/server/server_latency" || metricName == "grpc.io/client/api_latency" {
			// RPCs have a context timeout of 5s, so all the recorded
			// measurements (one per RPC - two total) should fall within 5
			// second buckets.
			if err := distributionDataLatencyCount(vi, 2, wantMetric.wantTags); err != nil {
				t.Fatalf("Invalid OpenCensus export view data for metric %v: %v", metricName, err)
			}
			continue
		}
		if diff := cmp.Diff(vi, wantMetric.wantVI); diff != "" {
			t.Fatalf("got unexpected viewInformation for metric %v, diff (-got, +want): %v", metricName, diff)
		}
		// Note that this test only fatals with one error if a metric fails.
		// This is fine, as all are expected to pass so if a single one fails
		// you can figure it out and iterate as needed.
	}
}

// TestOpenCensusTags tests this instrumentation code's ability to propagate
// OpenCensus tags across the wire. It also tests the server stats handler's
// functionality of adding the server method tag for the application to see. The
// test makes a Unary RPC without a tag map and with a tag map, and expects to
// see a tag map at the application layer with server method tag in the first
// case, and a tag map at the application layer with the populated tag map plus
// server method tag in second case.
func (s) TestOpenCensusTags(t *testing.T) {
	// This stub servers functions represent the application layer server side.
	// This is the intended feature being tested: that open census tags
	// populated at the client side application layer end up at the server side
	// application layer with the server method tag key in addition to the map
	// populated at the client side application layer if populated.
	tmCh := testutils.NewChannel()
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, _ *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			// Do the sends of the tag maps for assertions in this main testing
			// goroutine. Do the receives and assertions in a forked goroutine.
			if tm := tag.FromContext(ctx); tm != nil {
				tmCh.Send(tm)
			} else {
				tmCh.Send(errors.New("no tag map received server side"))
			}
			return &testpb.SimpleResponse{}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{ServerOption(TraceOptions{})}, DialOption(TraceOptions{})); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	key1 := tag.MustNewKey("key 1")
	wg := sync.WaitGroup{}
	wg.Add(1)
	readerErrCh := testutils.NewChannel()
	// Spawn a goroutine to receive and validation two tag maps received by the
	// server application code.
	go func() {
		defer wg.Done()
		unaryCallMethodName := "grpc.testing.TestService/UnaryCall"
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		// Attempt to receive the tag map from the first RPC.
		if tm, err := tmCh.Receive(ctx); err == nil {
			tagMap, ok := tm.(*tag.Map)
			// Shouldn't happen, this test sends only *tag.Map type on channel.
			if !ok {
				readerErrCh.Send(fmt.Errorf("received wrong type from channel: %T", tm))
			}
			// keyServerMethod should be present in this tag map received server
			// side.
			val, ok := tagMap.Value(keyServerMethod)
			if !ok {
				readerErrCh.Send(fmt.Errorf("no key: %v present in OpenCensus tag map", keyServerMethod.Name()))
			}
			if val != unaryCallMethodName {
				readerErrCh.Send(fmt.Errorf("serverMethod received: %v, want server method: %v", val, unaryCallMethodName))
			}
		} else {
			readerErrCh.Send(fmt.Errorf("error while waiting for a tag map: %v", err))
		}
		readerErrCh.Send(nil)

		// Attempt to receive the tag map from the second RPC.
		if tm, err := tmCh.Receive(ctx); err == nil {
			tagMap, ok := tm.(*tag.Map)
			// Shouldn't happen, this test sends only *tag.Map type on channel.
			if !ok {
				readerErrCh.Send(fmt.Errorf("received wrong type from channel: %T", tm))
			}
			// key1: "value1" populated in the tag map client side should make
			// its way to server.
			val, ok := tagMap.Value(key1)
			if !ok {
				readerErrCh.Send(fmt.Errorf("no key: %v present in OpenCensus tag map", key1.Name()))
			}
			if val != "value1" {
				readerErrCh.Send(fmt.Errorf("key %v received: %v, want server method: %v", key1.Name(), val, unaryCallMethodName))
			}
			// keyServerMethod should be appended to tag map as well.
			val, ok = tagMap.Value(keyServerMethod)
			if !ok {
				readerErrCh.Send(fmt.Errorf("no key: %v present in OpenCensus tag map", keyServerMethod.Name()))
			}
			if val != unaryCallMethodName {
				readerErrCh.Send(fmt.Errorf("key: %v received: %v, want server method: %v", keyServerMethod.Name(), val, unaryCallMethodName))
			}
		} else {
			readerErrCh.Send(fmt.Errorf("error while waiting for second tag map: %v", err))
		}
		readerErrCh.Send(nil)
	}()

	// Make a unary RPC without populating an OpenCensus tag map. The server
	// side should receive an OpenCensus tag map containing only the
	// keyServerMethod.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Should receive a nil error from the readerErrCh, meaning the reader
	// goroutine successfully received a tag map with the keyServerMethod
	// populated.
	if chErr, err := readerErrCh.Receive(ctx); chErr != nil || err != nil {
		if err != nil {
			t.Fatalf("Should have received something from error channel: %v", err)
		}
		if chErr != nil {
			t.Fatalf("Should have received a nil error from channel, instead received: %v", chErr)
		}
	}

	tm := &tag.Map{}
	ctx = tag.NewContext(ctx, tm)
	ctx, err := tag.New(ctx, tag.Upsert(key1, "value1"))
	// Setup steps like this can fatal, so easier to do the RPC's and subsequent
	// sends of the tag maps of the RPC's in main goroutine and have the
	// corresponding receives and assertions in a forked goroutine.
	if err != nil {
		t.Fatalf("Error creating tag map: %v", err)
	}
	// Make a unary RPC with a populated OpenCensus tag map. The server side
	// should receive an OpenCensus tag map containing this populated tag map
	// with the keyServerMethod tag appended to it.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	if chErr, err := readerErrCh.Receive(ctx); chErr != nil || err != nil {
		if err != nil {
			t.Fatalf("Should have received something from error channel: %v", err)
		}
		if chErr != nil {
			t.Fatalf("Should have received a nil error from channel, instead received: %v", chErr)
		}
	}
	wg.Wait()
}

// compareSpanContext only checks the equality of the trace options, which
// represent whether the span should be sampled. The other fields are checked
// for presence in later assertions.
func compareSpanContext(sc trace.SpanContext, sc2 trace.SpanContext) bool {
	return sc.TraceOptions.IsSampled() == sc2.TraceOptions.IsSampled()
}

func compareMessageEvents(me []trace.MessageEvent, me2 []trace.MessageEvent) bool {
	if len(me) != len(me2) {
		return false
	}
	// Order matters here, message events are deterministic so no flakiness to
	// test.
	for i, e := range me {
		e2 := me2[i]
		if e.EventType != e2.EventType {
			return false
		}
		if e.MessageID != e2.MessageID {
			return false
		}
		if e.UncompressedByteSize != e2.UncompressedByteSize {
			return false
		}
		if e.CompressedByteSize != e2.CompressedByteSize {
			return false
		}
	}
	return true
}

// compareLinks compares the type of link received compared to the wanted link.
func compareLinks(ls []trace.Link, ls2 []trace.Link) bool {
	if len(ls) != len(ls2) {
		return false
	}
	for i, l := range ls {
		l2 := ls2[i]
		if l.Type != l2.Type {
			return false
		}
	}
	return true
}

// spanInformation is the information received about the span. This is a subset
// of information that is important to verify that gRPC has knobs over, which
// goes through a stable OpenCensus API with well defined behavior. This keeps
// the robustness of assertions over time.
type spanInformation struct {
	// SpanContext either gets pulled off the wire in certain cases server side
	// or created.
	sc              trace.SpanContext
	parentSpanID    trace.SpanID
	spanKind        int
	name            string
	message         string
	messageEvents   []trace.MessageEvent
	status          trace.Status
	links           []trace.Link
	hasRemoteParent bool
	childSpanCount  int
}

// validateTraceAndSpanIDs checks for consistent trace ID across the full trace.
// It also asserts each span has a corresponding generated SpanID, and makes
// sure in the case of a server span and a client span, the server span points
// to the client span as its parent. This is assumed to be called with spans
// from the same RPC (thus the same trace). If called with spanInformation slice
// of length 2, it assumes first span is a server span which points to second
// span as parent and second span is a client span. These assertions are
// orthogonal to pure equality assertions, as this data is generated at runtime,
// so can only test relations between IDs (i.e. this part of the data has the
// same ID as this part of the data).
//
// Returns an error in the case of a failing assertion, non nil error otherwise.
func validateTraceAndSpanIDs(sis []spanInformation) error {
	var traceID trace.TraceID
	for i, si := range sis {
		// Trace IDs should all be consistent across every span, since this
		// function assumes called with Span from one RPC, which all fall under
		// one trace.
		if i == 0 {
			traceID = si.sc.TraceID
		} else {
			if !cmp.Equal(si.sc.TraceID, traceID) {
				return fmt.Errorf("TraceIDs should all be consistent: %v, %v", si.sc.TraceID, traceID)
			}
		}
		// Due to the span IDs being 8 bytes, the documentation states that it
		// is practically a mathematical uncertainty in practice to create two
		// colliding IDs. Thus, for a presence check (the ID was actually
		// generated, I will simply compare to the zero value, even though a
		// zero value is a theoretical possibility of generation). This is
		// because in practice, this zero value defined by this test will never
		// collide with the generated ID.
		if cmp.Equal(si.sc.SpanID, trace.SpanID{}) {
			return errors.New("span IDs should be populated from the creation of the span")
		}
	}
	// If the length of spans of an RPC is 2, it means there is a server span
	// which exports first and a client span which exports second. Thus, the
	// server span should point to the client span as its parent, represented
	// by its ID.
	if len(sis) == 2 {
		if !cmp.Equal(sis[0].parentSpanID, sis[1].sc.SpanID) {
			return fmt.Errorf("server span should point to the client span as its parent. parentSpanID: %v, clientSpanID: %v", sis[0].parentSpanID, sis[1].sc.SpanID)
		}
	}
	return nil
}

// Equal compares the constant data of the exported span information that is
// important for correctness known before runtime.
func (si spanInformation) Equal(si2 spanInformation) bool {
	if !compareSpanContext(si.sc, si2.sc) {
		return false
	}

	if si.spanKind != si2.spanKind {
		return false
	}
	if si.name != si2.name {
		return false
	}
	if si.message != si2.message {
		return false
	}
	// Ignore attribute comparison because Java doesn't even populate any so not
	// important for correctness.
	if !compareMessageEvents(si.messageEvents, si2.messageEvents) {
		return false
	}
	if !cmp.Equal(si.status, si2.status) {
		return false
	}
	// compare link type as link type child is important.
	if !compareLinks(si.links, si2.links) {
		return false
	}
	if si.hasRemoteParent != si2.hasRemoteParent {
		return false
	}
	return si.childSpanCount == si2.childSpanCount
}

func (fe *fakeExporter) ExportSpan(sd *trace.SpanData) {
	fe.mu.Lock()
	defer fe.mu.Unlock()

	// Persist the subset of data received that is important for correctness and
	// to make various assertions on later. Keep the ordering as ordering of
	// spans is deterministic in the context of one RPC.
	gotSI := spanInformation{
		sc:           sd.SpanContext,
		parentSpanID: sd.ParentSpanID,
		spanKind:     sd.SpanKind,
		name:         sd.Name,
		message:      sd.Message,
		// annotations - ignore
		// attributes - ignore, I just left them in from previous but no spec
		// for correctness so no need to test. Java doesn't even have any
		// attributes.
		messageEvents:   sd.MessageEvents,
		status:          sd.Status,
		links:           sd.Links,
		hasRemoteParent: sd.HasRemoteParent,
		childSpanCount:  sd.ChildSpanCount,
	}
	fe.seenSpans = append(fe.seenSpans, gotSI)
}

// waitForServerSpan waits until a server span appears somewhere in the span
// list in an exporter. Returns an error if no server span found within the
// passed context's timeout.
func waitForServerSpan(ctx context.Context, fe *fakeExporter) error {
	for ; ctx.Err() == nil; <-time.After(time.Millisecond) {
		fe.mu.Lock()
		for _, seenSpan := range fe.seenSpans {
			if seenSpan.spanKind == trace.SpanKindServer {
				fe.mu.Unlock()
				return nil
			}
		}
		fe.mu.Unlock()
	}
	return fmt.Errorf("timeout when waiting for server span to be present in exporter")
}

// TestSpan tests emitted spans from gRPC. It configures a system with a gRPC
// Client and gRPC server with the OpenCensus Dial and Server Option configured,
// and makes a Unary RPC and a Streaming RPC. This should cause spans with
// certain information to be emitted from client and server side for each RPC.
func (s) TestSpan(t *testing.T) {
	fe := &fakeExporter{
		t: t,
	}
	trace.RegisterExporter(fe)
	defer trace.UnregisterExporter(fe)

	so := TraceOptions{
		TS:           trace.ProbabilitySampler(1),
		DisableTrace: false,
	}
	ss := &stubserver.StubServer{
		UnaryCallF: func(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			for {
				_, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
			}
		},
	}
	if err := ss.Start([]grpc.ServerOption{ServerOption(so)}, DialOption(so)); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Make a Unary RPC. This should cause a span with message events
	// corresponding to the request message and response message to be emitted
	// both from the client and the server.
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: &testpb.Payload{}}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
	wantSI := []spanInformation{
		{
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			name: "Attempt.grpc.testing.TestService.UnaryCall",
			messageEvents: []trace.MessageEvent{
				{
					EventType:            trace.MessageEventTypeSent,
					MessageID:            1, // First msg send so 1 (see comment above)
					UncompressedByteSize: 2,
					CompressedByteSize:   2,
				},
				{
					EventType: trace.MessageEventTypeRecv,
					MessageID: 1, // First msg recv so 1 (see comment above)
				},
			},
			hasRemoteParent: false,
		},
		{
			// Sampling rate of 100 percent, so this should populate every span
			// with the information that this span is being sampled. Here and
			// every other span emitted in this test.
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			spanKind: trace.SpanKindServer,
			name:     "grpc.testing.TestService.UnaryCall",
			// message id - "must be calculated as two different counters
			// starting from 1 one for sent messages and one for received
			// message. This way we guarantee that the values will be consistent
			// between different implementations. In case of unary calls only
			// one sent and one received message will be recorded for both
			// client and server spans."
			messageEvents: []trace.MessageEvent{
				{
					EventType:            trace.MessageEventTypeRecv,
					MessageID:            1, // First msg recv so 1 (see comment above)
					UncompressedByteSize: 2,
					CompressedByteSize:   2,
				},
				{
					EventType: trace.MessageEventTypeSent,
					MessageID: 1, // First msg send so 1 (see comment above)
				},
			},
			links: []trace.Link{
				{
					Type: trace.LinkTypeChild,
				},
			},
			// For some reason, status isn't populated in the data sent to the
			// exporter. This seems wrong, but it didn't send status in old
			// instrumentation code, so I'm iffy on it but fine.
			hasRemoteParent: true,
		},
		{
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			spanKind:        trace.SpanKindClient,
			name:            "grpc.testing.TestService.UnaryCall",
			hasRemoteParent: false,
			childSpanCount:  1,
		},
	}
	if err := waitForServerSpan(ctx, fe); err != nil {
		t.Fatal(err)
	}
	var spanInfoSort = func(i, j int) bool {
		// This will order into attempt span (which has an unset span kind to
		// not prepend Sent. to span names in backends), then call span, then
		// server span.
		return fe.seenSpans[i].spanKind < fe.seenSpans[j].spanKind
	}
	fe.mu.Lock()
	// Sort the underlying seen Spans for cmp.Diff assertions and ID
	// relationship assertions.
	sort.Slice(fe.seenSpans, spanInfoSort)
	if diff := cmp.Diff(fe.seenSpans, wantSI); diff != "" {
		fe.mu.Unlock()
		t.Fatalf("got unexpected spans, diff (-got, +want): %v", diff)
	}
	if err := validateTraceAndSpanIDs(fe.seenSpans); err != nil {
		fe.mu.Unlock()
		t.Fatalf("Error in runtime data assertions: %v", err)
	}
	if !cmp.Equal(fe.seenSpans[1].parentSpanID, fe.seenSpans[0].sc.SpanID) {
		t.Fatalf("server span should point to the client attempt span as its parent. parentSpanID: %v, clientAttemptSpanID: %v", fe.seenSpans[1].parentSpanID, fe.seenSpans[0].sc.SpanID)
	}
	if !cmp.Equal(fe.seenSpans[0].parentSpanID, fe.seenSpans[2].sc.SpanID) {
		t.Fatalf("client attempt span should point to the client call span as its parent. parentSpanID: %v, clientCallSpanID: %v", fe.seenSpans[0].parentSpanID, fe.seenSpans[2].sc.SpanID)
	}

	fe.seenSpans = nil
	fe.mu.Unlock()

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.FullDuplexCall failed: %v", err)
	}
	// Send two messages. This should be recorded in the emitted spans message
	// events, with message IDs which increase for each message.
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}
	if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
		t.Fatalf("stream.Send failed: %v", err)
	}

	stream.CloseSend()
	if _, err = stream.Recv(); err != io.EOF {
		t.Fatalf("unexpected error: %v, expected an EOF error", err)
	}

	wantSI = []spanInformation{
		{
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			name: "Attempt.grpc.testing.TestService.FullDuplexCall",
			messageEvents: []trace.MessageEvent{
				{
					EventType: trace.MessageEventTypeSent,
					MessageID: 1, // First msg send so 1
				},
				{
					EventType: trace.MessageEventTypeSent,
					MessageID: 2, // Second msg send so 2
				},
			},
			hasRemoteParent: false,
		},
		{
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			spanKind: trace.SpanKindServer,
			name:     "grpc.testing.TestService.FullDuplexCall",
			links: []trace.Link{
				{
					Type: trace.LinkTypeChild,
				},
			},
			messageEvents: []trace.MessageEvent{
				{
					EventType: trace.MessageEventTypeRecv,
					MessageID: 1, // First msg recv so 1
				},
				{
					EventType: trace.MessageEventTypeRecv,
					MessageID: 2, // Second msg recv so 2
				},
			},
			hasRemoteParent: true,
		},
		{
			sc: trace.SpanContext{
				TraceOptions: 1,
			},
			spanKind:        trace.SpanKindClient,
			name:            "grpc.testing.TestService.FullDuplexCall",
			hasRemoteParent: false,
			childSpanCount:  1,
		},
	}
	if err := waitForServerSpan(ctx, fe); err != nil {
		t.Fatal(err)
	}
	fe.mu.Lock()
	defer fe.mu.Unlock()
	// Sort the underlying seen Spans for cmp.Diff assertions and ID
	// relationship assertions.
	sort.Slice(fe.seenSpans, spanInfoSort)
	if diff := cmp.Diff(fe.seenSpans, wantSI); diff != "" {
		t.Fatalf("got unexpected spans, diff (-got, +want): %v", diff)
	}
	if err := validateTraceAndSpanIDs(fe.seenSpans); err != nil {
		t.Fatalf("Error in runtime data assertions: %v", err)
	}
	if !cmp.Equal(fe.seenSpans[1].parentSpanID, fe.seenSpans[0].sc.SpanID) {
		t.Fatalf("server span should point to the client attempt span as its parent. parentSpanID: %v, clientAttemptSpanID: %v", fe.seenSpans[1].parentSpanID, fe.seenSpans[0].sc.SpanID)
	}
	if !cmp.Equal(fe.seenSpans[0].parentSpanID, fe.seenSpans[2].sc.SpanID) {
		t.Fatalf("client attempt span should point to the client call span as its parent. parentSpanID: %v, clientCallSpanID: %v", fe.seenSpans[0].parentSpanID, fe.seenSpans[2].sc.SpanID)
	}
}
