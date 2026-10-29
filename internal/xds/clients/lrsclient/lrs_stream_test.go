/*
 *
 * Copyright 2026 gRPC authors.
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

package lrsclient_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/internal/xds/clients/lrsclient"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

// TestReportLoad_SubsequentResponse_Clusters tests that receiving subsequent
// LoadStatsResponse messages updates the cluster set reported by the client on
// an active stream.
func (s) TestReportLoad_SubsequentResponse_Clusters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	config := lrsclient.Config{
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		ssCtx, ssCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer ssCancel()
		loadStore.Stop(ssCtx)
	}()

	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	// Wait for the initial request.
	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}

	// Record load for cluster1 and cluster2.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")

	// First response: report only cluster1.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{"cluster1"},
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Verify the first load report only contains cluster1.
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for first load report: %v", err)
	}
	gotStats := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if len(gotStats) != 1 || gotStats[0].GetClusterName() != "cluster1" {
		t.Fatalf("First load report got clusters %+v, want only cluster1", gotStats)
	}

	// Record load for both clusters again.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")

	// Second response on the same stream: switch to reporting only cluster2.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			Clusters:              []string{"cluster2"},
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Verify the subsequent load report only contains cluster2.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for second load report: %v", err)
	}
	gotStats = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if len(gotStats) != 1 || gotStats[0].GetClusterName() != "cluster2" {
		t.Fatalf("Second load report got clusters %+v, want only cluster2", gotStats)
	}

	// Record load for both clusters again.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("drop2")

	// Third response on the same stream: switch to send_all_clusters.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Verify the subsequent load report contains both cluster1 and cluster2.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for third load report: %v", err)
	}
	gotStats = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if len(gotStats) != 2 {
		t.Fatalf("Third load report got %d clusters, want 2 (cluster1 and cluster2)", len(gotStats))
	}
}

// TestReportLoad_SubsequentResponse_Interval tests that receiving subsequent
// LoadStatsResponse messages updates the reporting interval on an active stream.
func (s) TestReportLoad_SubsequentResponse_Interval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	config := lrsclient.Config{
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		ssCtx, ssCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer ssCancel()
		loadStore.Stop(ssCtx)
	}()

	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}

	// Initial response with 200ms interval.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop1")
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 200000000}, // 200ms
		},
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for first load report: %v", err)
	}

	// Subsequent response on active stream updating interval to 30ms.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("drop2")
	startTime := time.Now()
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 30000000}, // 30ms
		},
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for second load report: %v", err)
	}
	elapsed := time.Since(startTime)
	// With the updated 30ms interval, the report should arrive well before the
	// old 200ms interval.
	if elapsed > 150*time.Millisecond {
		t.Errorf("Subsequent report took %v, want < 150ms after interval update to 30ms", elapsed)
	}
}

// TestReportLoad_ServerClosesStream_ImmediateReconnection tests that when the
// server terminates the active stream, the client detects the termination
// immediately through the receiver loop and initiates a new stream after backoff
// rather than waiting for the next periodic report send.
func (s) TestReportLoad_ServerClosesStream_ImmediateReconnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	config := lrsclient.Config{
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		ssCtx, ssCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer ssCancel()
		loadStore.Stop(ssCtx)
	}()

	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}

	// Server responds with a very long load reporting interval (10s).
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Seconds: 10},
		},
	}

	// Server now closes the active stream by sending an error.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Err: status.Error(codes.Unavailable, "server closing stream"),
	}

	// The client receiver loop should notice the stream termination immediately
	// without waiting for the 10-second ticker, closing the stream.
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for LRS stream to close after server error: %v", err)
	}

	// The client should reconnect and open a new stream after backoff.
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for client to recreate LRS stream after server closure: %v", err)
	}

	// And the new initial request should be received.
	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for initial request on new stream: %v", err)
	}
}

// TestReportLoad_SubsequentResponse_InvalidInterval tests that receiving a
// subsequent response with an invalid interval terminates the stream so the
// client reconnects after backoff.
func (s) TestReportLoad_SubsequentResponse_InvalidInterval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	configs := map[string]grpctransport.Config{"insecure": {Credentials: insecure.NewBundle()}}
	config := lrsclient.Config{
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(configs),
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer func() {
		ssCtx, ssCancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer ssCancel()
		loadStore.Stop(ssCtx)
	}()

	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	if _, err := lrsServer.LRSRequestChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}

	// Initial valid response.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Subsequent response with negative interval.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Seconds: -1},
		},
	}

	// Stream should be closed due to invalid subsequent response.
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for LRS stream to close after invalid subsequent response: %v", err)
	}

	// Client should reconnect after backoff.
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for client to recreate LRS stream: %v", err)
	}
}
