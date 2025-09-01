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
 *
 */

package lrsclient_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/e2e"
	"google.golang.org/grpc/internal/xds/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/internal/xds/clients/lrsclient"
	lrsclientinternal "google.golang.org/grpc/internal/xds/clients/lrsclient/internal"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	testKey1                      = "test-key1"
	testKey2                      = "test-key2"
	defaultTestWatchExpiryTimeout = 100 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
)

var (
	testLocality1     = clients.Locality{Region: "test-region1"}
	testLocality2     = clients.Locality{Region: "test-region2"}
	toleranceCmpOpt   = cmpopts.EquateApprox(0, 1e-5)
	ignoreOrderCmpOpt = protocmp.FilterField(&v3endpointpb.ClusterStats{}, "upstream_locality_stats",
		cmpopts.SortSlices(func(a, b protocmp.Message) bool {
			return a.String() < b.String()
		}),
	)
)

type wrappedListener struct {
	net.Listener
	newConnChan *testutils.Channel // Connection attempts are pushed here.
}

func (wl *wrappedListener) Accept() (net.Conn, error) {
	c, err := wl.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wl.newConnChan.Send(struct{}{})
	return c, err
}

// Tests a load reporting scenario where the LRS client is reporting loads to
// multiple servers. Verifies the following:
//   - calling the load reporting API with different server configuration
//     results in connections being created to those corresponding servers
//   - the same load.Store is not returned when the load reporting API called
//     with different server configurations
//   - canceling the load reporting from the client results in the LRS stream
//     being canceled on the server
func (s) TestReportLoad_ConnectionCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create two management servers that also serve LRS.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	newConnChan1 := testutils.NewChannelWithSize(1)
	lis1 := &wrappedListener{
		Listener:    l,
		newConnChan: newConnChan1,
	}
	mgmtServer1 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis1,
		SupportLoadReportingService: true,
	})
	l, err = net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	newConnChan2 := testutils.NewChannelWithSize(1)
	lis2 := &wrappedListener{
		Listener:    l,
		newConnChan: newConnChan2,
	}
	mgmtServer2 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis2,
		SupportLoadReportingService: true,
	})

	// Create an LRS client with a configuration that contains both of
	// the above two servers. The authority name is immaterial here since load
	// reporting is per-server and not per-authority.
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

	serverIdentifier1 := clients.ServerIdentifier{ServerURI: mgmtServer1.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore1, err := client.ReportLoad(serverIdentifier1)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	ssCtx, ssCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ssCancel()
	defer loadStore1.Stop(ssCtx)

	// Call the load reporting API to report load to the first management
	// server, and ensure that a connection to the server is created.
	if _, err := newConnChan1.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for a connection to the first management server, after starting load reporting")
	}
	if _, err := mgmtServer1.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for LRS stream to be created")
	}

	// Call the load reporting API to report load to the first management
	// server, and ensure that a connection to the server is created.
	serverIdentifier2 := clients.ServerIdentifier{ServerURI: mgmtServer2.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore2, err := client.ReportLoad(serverIdentifier2)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	if _, err := newConnChan2.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for a connection to the second management server, after starting load reporting")
	}
	if _, err := mgmtServer2.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for LRS stream to be created")
	}

	if loadStore1 == loadStore2 {
		t.Fatalf("Got same store for different servers, want different")
	}

	// Push some loads on the received store.
	loadStore2.ReporterForCluster("cluster", "eds").CallDropped("test")

	// Ensure the initial load reporting request is received at the server.
	lrsServer := mgmtServer2.LRSServer
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw", "envoy.lrs.supports_send_all_clusters"},
	}
	wantInitialReq := &v3lrspb.LoadStatsRequest{Node: nodeProto}
	if diff := cmp.Diff(gotInitialReq, wantInitialReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in initial LRS request (-got, +want):\n%s", diff)
	}

	// Send a response from the server with a small deadline.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Ensure that loads are seen on the server.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for LRS request with loads: %v", err)
	}
	gotLoad := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if l := len(gotLoad); l != 1 {
		t.Fatalf("Received load for %d clusters, want 1", l)
	}

	// This field is set by the client to indicate the actual time elapsed since
	// the last report was sent. We cannot deterministically compare this, and
	// we cannot use the cmpopts.IgnoreFields() option on proto structs, since
	// we already use the protocmp.Transform() which marshals the struct into
	// another message. Hence setting this field to nil is the best option here.
	gotLoad[0].LoadReportInterval = nil
	wantLoad := &v3endpointpb.ClusterStats{
		ClusterName:          "cluster",
		ClusterServiceName:   "eds",
		TotalDroppedRequests: 1,
		DroppedRequests:      []*v3endpointpb.ClusterStats_DroppedRequests{{Category: "test", DroppedCount: 1}},
	}
	if diff := cmp.Diff(wantLoad, gotLoad[0], protocmp.Transform(), toleranceCmpOpt, ignoreOrderCmpOpt); diff != "" {
		t.Fatalf("Unexpected diff in LRS request (-got, +want):\n%s", diff)
	}

	// Stop this load reporting stream, server should see error canceled.
	ssCtx, ssCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ssCancel()
	loadStore2.Stop(ssCtx)

	// Server should receive a stream canceled error. There may be additional
	// load reports from the client in the channel.
	for {
		if ctx.Err() != nil {
			t.Fatal("Timeout when waiting for the LRS stream to be canceled on the server")
		}
		u, err := lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			continue
		}
		// Ignore load reports sent before the stream was cancelled.
		if u.(*fakeserver.Request).Err == nil {
			continue
		}
		if status.Code(u.(*fakeserver.Request).Err) != codes.Canceled {
			t.Fatalf("Unexpected LRS request: %v, want error canceled", u)
		}
		break
	}
}

// Tests a load reporting scenario where the load reporting API is called
// multiple times for the same server. The test verifies the following:
//   - calling the load reporting API the second time for the same server
//     configuration does not create a new LRS stream
//   - the LRS stream is closed *only* after all the API calls invoke their
//     cancel functions
//   - creating new streams after the previous one was closed works
func (s) TestReportLoad_StreamCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a management server that serves LRS.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create an LRS client with configuration pointing to the above server.
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

	// Call the load reporting API, and ensure that an LRS stream is created.
	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore1, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	// Push some loads on the received store.
	loadStore1.ReporterForCluster("cluster1", "eds1").CallDropped("test")
	loadStore1.ReporterForCluster("cluster1", "eds1").CallStarted(testLocality1)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality1, testKey1, 3.14)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality1, testKey1, 2.718)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallFinished(testLocality1, nil)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallStarted(testLocality2)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality2, testKey2, 1.618)
	loadStore1.ReporterForCluster("cluster1", "eds1").CallFinished(testLocality2, nil)

	// Ensure the initial load reporting request is received at the server.
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw", "envoy.lrs.supports_send_all_clusters"},
	}
	wantInitialReq := &v3lrspb.LoadStatsRequest{Node: nodeProto}
	if diff := cmp.Diff(gotInitialReq, wantInitialReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in initial LRS request (-got, +want):\n%s", diff)
	}

	// Send a response from the server with a small deadline.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Ensure that loads are seen on the server.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for LRS request with loads")
	}
	gotLoad := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if l := len(gotLoad); l != 1 {
		t.Fatalf("Received load for %d clusters, want 1", l)
	}

	// This field is set by the client to indicate the actual time elapsed since
	// the last report was sent. We cannot deterministically compare this, and
	// we cannot use the cmpopts.IgnoreFields() option on proto structs, since
	// we already use the protocmp.Transform() which marshals the struct into
	// another message. Hence setting this field to nil is the best option here.
	gotLoad[0].LoadReportInterval = nil
	wantLoad := &v3endpointpb.ClusterStats{
		ClusterName:          "cluster1",
		ClusterServiceName:   "eds1",
		TotalDroppedRequests: 1,
		DroppedRequests:      []*v3endpointpb.ClusterStats_DroppedRequests{{Category: "test", DroppedCount: 1}},
		UpstreamLocalityStats: []*v3endpointpb.UpstreamLocalityStats{
			{
				Locality: &v3corepb.Locality{Region: "test-region1"},
				LoadMetricStats: []*v3endpointpb.EndpointLoadMetricStats{
					// TotalMetricValue is the aggregation of 3.14 + 2.718 = 5.858
					{MetricName: testKey1, NumRequestsFinishedWithMetric: 2, TotalMetricValue: 5.858}},
				TotalSuccessfulRequests: 1,
				TotalIssuedRequests:     1,
			},
			{
				Locality: &v3corepb.Locality{Region: "test-region2"},
				LoadMetricStats: []*v3endpointpb.EndpointLoadMetricStats{
					{MetricName: testKey2, NumRequestsFinishedWithMetric: 1, TotalMetricValue: 1.618}},
				TotalSuccessfulRequests: 1,
				TotalIssuedRequests:     1,
			},
		},
	}
	if diff := cmp.Diff(wantLoad, gotLoad[0], protocmp.Transform(), toleranceCmpOpt, ignoreOrderCmpOpt); diff != "" {
		t.Fatalf("Unexpected diff in LRS request (-got, +want):\n%s", diff)
	}

	// Make another call to the load reporting API, and ensure that a new LRS
	// stream is not created.
	loadStore2, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamOpenChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("New LRS stream created when expected to use an existing one")
	}

	// Push more loads.
	loadStore2.ReporterForCluster("cluster2", "eds2").CallDropped("test")

	// Ensure that loads are seen on the server. We need a loop here because
	// there could have been some requests from the client in the time between
	// us reading the first request and now. Those would have been queued in the
	// request channel that we read out of.
	for {
		if ctx.Err() != nil {
			t.Fatalf("Timeout when waiting for new loads to be seen on the server")
		}

		req, err = lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil {
			continue
		}
		gotLoad = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if l := len(gotLoad); l != 1 {
			continue
		}
		gotLoad[0].LoadReportInterval = nil
		wantLoad := &v3endpointpb.ClusterStats{
			ClusterName:          "cluster2",
			ClusterServiceName:   "eds2",
			TotalDroppedRequests: 1,
			DroppedRequests:      []*v3endpointpb.ClusterStats_DroppedRequests{{Category: "test", DroppedCount: 1}},
		}
		if diff := cmp.Diff(wantLoad, gotLoad[0], protocmp.Transform()); diff != "" {
			t.Logf("Unexpected diff in LRS request (-got, +want):\n%s", diff)
			continue
		}
		break
	}

	// Cancel the first load reporting call, and ensure that the stream does not
	// close (because we have another call open).
	ssCtx, ssCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ssCancel()
	loadStore1.Stop(ssCtx)
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamCloseChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("LRS stream closed when expected to stay open")
	}

	// Stop the second load reporting call, and ensure the stream is closed.
	ssCtx, ssCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ssCancel()
	loadStore2.Stop(ssCtx)
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatal("Timeout waiting for LRS stream to close")
	}

	// Calling the load reporting API again should result in the creation of a
	// new LRS stream. This ensures that creating and closing multiple streams
	// works smoothly.
	loadStore3, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}
	ssCtx, ssCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer ssCancel()
	loadStore3.Stop(ssCtx)
}

// TestReportLoad_StopWithContext tests the behavior of LoadStore.Stop() when
// called with a context. It verifies that:
//   - Stop() blocks until the context expires or final load send attempt is
//     made.
//   - Final load report is seen on the server after stop is called.
//   - The stream is closed after Stop() returns.
func (s) TestReportLoad_StopWithContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Create a management server that serves LRS.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create an LRS client with configuration pointing to the above server.
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

	// Call the load reporting API, and ensure that an LRS stream is created.
	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	// Push some loads on the received store.
	loadStore.ReporterForCluster("cluster1", "eds1").CallDropped("test")

	// Ensure the initial load reporting request is received at the server.
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw", "envoy.lrs.supports_send_all_clusters"},
	}
	wantInitialReq := &v3lrspb.LoadStatsRequest{Node: nodeProto}
	if diff := cmp.Diff(gotInitialReq, wantInitialReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in initial LRS request (-got, +want):\n%s", diff)
	}

	// Send a response from the server with a small deadline.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Ensure that loads are seen on the server.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for LRS request with loads")
	}
	gotLoad := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if l := len(gotLoad); l != 1 {
		t.Fatalf("Received load for %d clusters, want 1", l)
	}

	// This field is set by the client to indicate the actual time elapsed since
	// the last report was sent. We cannot deterministically compare this, and
	// we cannot use the cmpopts.IgnoreFields() option on proto structs, since
	// we already use the protocmp.Transform() which marshals the struct into
	// another message. Hence setting this field to nil is the best option here.
	gotLoad[0].LoadReportInterval = nil
	wantLoad := &v3endpointpb.ClusterStats{
		ClusterName:          "cluster1",
		ClusterServiceName:   "eds1",
		TotalDroppedRequests: 1,
		DroppedRequests:      []*v3endpointpb.ClusterStats_DroppedRequests{{Category: "test", DroppedCount: 1}},
	}
	if diff := cmp.Diff(wantLoad, gotLoad[0], protocmp.Transform(), toleranceCmpOpt, ignoreOrderCmpOpt); diff != "" {
		t.Fatalf("Unexpected diff in LRS request (-got, +want):\n%s", diff)
	}

	// Create a context for Stop() that remains until the end of test to ensure
	// that only possibility of Stop()s to finish is if final load send attempt
	// is made. If final load attempt is not made, test will timeout.
	stopCtx, stopCancel := context.WithCancel(ctx)
	defer stopCancel()

	// Push more loads.
	loadStore.ReporterForCluster("cluster2", "eds2").CallDropped("test")

	stopCalled := make(chan struct{})
	// Call Stop in a separate goroutine. It will block until
	// final load send attempt is made.
	go func() {
		loadStore.Stop(stopCtx)
		close(stopCalled)
	}()
	<-stopCalled

	// Ensure that loads are seen on the server. We need a loop here because
	// there could have been some requests from the client in the time between
	// us reading the first request and now. Those would have been queued in the
	// request channel that we read out of.
	for {
		if ctx.Err() != nil {
			t.Fatalf("Timeout when waiting for new loads to be seen on the server")
		}

		req, err = lrsServer.LRSRequestChan.Receive(ctx)
		if err != nil || req.(*fakeserver.Request).Err != nil {
			continue
		}
		if req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest) == nil {
			// This can happen due to a race:
			// 1. Load for "cluster2" is reported just before Stop().
			// 2. The periodic ticker might send this load before Stop()'s
			//    final send mechanism processes it, clearing the data.
			// 3. Stop()'s final send might then send an empty report.
			//    This is acceptable for this test because we only need to verify
			//    if the final load report send attempt was made.
			t.Logf("Empty final load report sent on server")
			break
		}
		gotLoad = req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
		if l := len(gotLoad); l != 1 {
			continue
		}
		gotLoad[0].LoadReportInterval = nil
		wantLoad := &v3endpointpb.ClusterStats{
			ClusterName:          "cluster2",
			ClusterServiceName:   "eds2",
			TotalDroppedRequests: 1,
			DroppedRequests:      []*v3endpointpb.ClusterStats_DroppedRequests{{Category: "test", DroppedCount: 1}},
		}
		if diff := cmp.Diff(wantLoad, gotLoad[0], protocmp.Transform()); diff != "" {
			t.Logf("Unexpected diff in LRS request (-got, +want):\n%s", diff)
			continue
		}
		break
	}

	// Verify the stream is eventually closed on the server side.
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatal("Timeout waiting for LRS stream to close")
	}
}

// TestReportLoad_LoadReportInterval tests verify that the load report interval
// received by the LRS server is the duration between start of last load
// reporting by the client and the time when the load is reported to the LRS
// server.
func (s) TestReportLoad_LoadReportInterval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	originalTimeNow := lrsclientinternal.TimeNow
	t.Cleanup(func() { lrsclientinternal.TimeNow = originalTimeNow })

	// Create a management server that serves LRS.
	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})

	// Create an LRS client with configuration pointing to the above server.
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

	// Call the load reporting API, and ensure that an LRS stream is created.
	serverIdentifier := clients.ServerIdentifier{ServerURI: mgmtServer.Address, Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}
	loadStore1, err := client.ReportLoad(serverIdentifier)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	// Initial time for reporter creation
	currentTime := time.Now()
	lrsclientinternal.TimeNow = func() time.Time {
		return currentTime
	}

	// Report dummy drop to ensure stats is not nil.
	loadStore1.ReporterForCluster("cluster1", "eds1").CallDropped("test")

	// Update currentTime to simulate the passage of time between the reporter
	// creation and first stats() call.
	currentTime = currentTime.Add(5 * time.Second)

	// Ensure the initial load reporting request is received at the server.
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "user-agent",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: "0.0.0.0"},
		ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw", "envoy.lrs.supports_send_all_clusters"},
	}
	wantInitialReq := &v3lrspb.LoadStatsRequest{Node: nodeProto}
	if diff := cmp.Diff(gotInitialReq, wantInitialReq, protocmp.Transform()); diff != "" {
		t.Fatalf("Unexpected diff in initial LRS request (-got, +want):\n%s", diff)
	}

	// Send a response from the server with a small deadline.
	lrsServer.LRSResponseChan <- &fakeserver.Response{
		Resp: &v3lrspb.LoadStatsResponse{
			SendAllClusters:       true,
			LoadReportingInterval: &durationpb.Duration{Nanos: 50000000}, // 50ms
		},
	}

	// Ensure that loads are seen on the server.
	req, err = lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatal("Timeout when waiting for LRS request with loads")
	}
	gotLoad := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest).ClusterStats
	if l := len(gotLoad); l != 1 {
		t.Fatalf("Received load for %d clusters, want 1", l)
	}
	// Verify load received at LRS server has load report interval calculated
	// from the time of reporter creation.
	if got, want := gotLoad[0].GetLoadReportInterval().AsDuration(), 5*time.Second; got != want {
		t.Errorf("Got load report interval %v, want %v", got, want)
	}

	ssCtx, ssCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer ssCancel()
	loadStore1.Stop(ssCtx)
}
