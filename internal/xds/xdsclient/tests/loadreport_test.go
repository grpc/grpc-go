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

package xdsclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/internal/testutils/xds/fakeserver"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3lrspb "github.com/envoyproxy/go-control-plane/envoy/service/load_stats/v3"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	testKey1 = "test-key1"
	testKey2 = "test-key2"
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

// Tests a load reporting scenario where the xDS client is reporting loads to
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
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local TCP listener: %v", err)
	}
	newConnChan1 := testutils.NewChannel()
	lis1 := &wrappedListener{
		Listener:    l,
		newConnChan: newConnChan1,
	}
	mgmtServer1 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis1,
		SupportLoadReportingService: true,
	})
	l, err = testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("Failed to create a local TCP listener: %v", err)
	}
	newConnChan2 := testutils.NewChannel()
	lis2 := &wrappedListener{
		Listener:    l,
		newConnChan: newConnChan2,
	}
	mgmtServer2 := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
		Listener:                    lis2,
		SupportLoadReportingService: true,
	})

	// Create an xDS client with a bootstrap configuration that contains both of
	// the above two servers. The authority name is immaterial here since load
	// reporting is per-server and not per-authority.
	nodeID := uuid.New().String()
	bc, err := bootstrap.NewContentsForTesting(bootstrap.ConfigOptionsForTesting{
		Servers: []byte(fmt.Sprintf(`[{
			"server_uri": %q,
			"channel_creds": [{"type": "insecure"}]
		}]`, mgmtServer1.Address)),
		Node: []byte(fmt.Sprintf(`{"id": "%s"}`, nodeID)),
		Authorities: map[string]json.RawMessage{
			"test-authority": []byte(fmt.Sprintf(`{
				"xds_servers": [{
					"server_uri": %q,
					"channel_creds": [{"type": "insecure"}]
				}]}`, mgmtServer2.Address)),
		},
	})
	if err != nil {
		t.Fatalf("Failed to create bootstrap configuration: %v", err)
	}
	client := createXDSClient(t, bc)

	serverCfg1, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: mgmtServer1.Address})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}
	// Call the load reporting API to report load to the first management
	// server, and ensure that a connection to the server is created.
	store1, lrsCancel1 := client.ReportLoad(serverCfg1)
	sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel()
	defer lrsCancel1(sCtx)
	if _, err := newConnChan1.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for a connection to the first management server, after starting load reporting")
	}
	if _, err := mgmtServer1.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for LRS stream to be created")
	}

	serverCfg2, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: mgmtServer2.Address})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}
	// Call the load reporting API to report load to the second management
	// server, and ensure that a connection to the server is created.
	store2, lrsCancel2 := client.ReportLoad(serverCfg2)
	sCtx2, sCancel2 := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel2()
	defer lrsCancel2(sCtx2)
	if _, err := newConnChan2.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for a connection to the second management server, after starting load reporting")
	}
	if _, err := mgmtServer2.LRSServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatal("Timeout when waiting for LRS stream to be created")
	}

	if store1 == store2 {
		t.Fatalf("Got same store for different servers, want different")
	}

	// Push some loads on the received store.
	store2.ReporterForCluster("cluster", "eds").CallDropped("test")

	// Ensure the initial load reporting request is received at the server.
	lrsServer := mgmtServer2.LRSServer
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "gRPC Go",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
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

	// Cancel this load reporting stream, server should see error canceled.
	sCtx2, sCancel2 = context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel2()
	lrsCancel2(sCtx2)

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

	// Create an xDS client with bootstrap pointing to the above server.
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	client := createXDSClient(t, bc)

	// Call the load reporting API, and ensure that an LRS stream is created.
	serverConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: mgmtServer.Address})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}
	store1, cancel1 := client.ReportLoad(serverConfig)
	lrsServer := mgmtServer.LRSServer
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}

	// Push some loads on the received store.
	store1.ReporterForCluster("cluster1", "eds1").CallDropped("test")
	store1.ReporterForCluster("cluster1", "eds1").CallStarted(testLocality1)
	store1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality1, testKey1, 3.14)
	store1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality1, testKey1, 2.718)
	store1.ReporterForCluster("cluster1", "eds1").CallFinished(testLocality1, nil)
	store1.ReporterForCluster("cluster1", "eds1").CallStarted(testLocality2)
	store1.ReporterForCluster("cluster1", "eds1").CallServerLoad(testLocality2, testKey2, 1.618)
	store1.ReporterForCluster("cluster1", "eds1").CallFinished(testLocality2, nil)

	// Ensure the initial load reporting request is received at the server.
	req, err := lrsServer.LRSRequestChan.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout when waiting for initial LRS request: %v", err)
	}
	gotInitialReq := req.(*fakeserver.Request).Req.(*v3lrspb.LoadStatsRequest)
	nodeProto := &v3corepb.Node{
		Id:                   nodeID,
		UserAgentName:        "gRPC Go",
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
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
	store2, cancel2 := client.ReportLoad(serverConfig)
	sCtx, sCancel := context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamOpenChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("New LRS stream created when expected to use an existing one")
	}

	// Push more loads.
	store2.ReporterForCluster("cluster2", "eds2").CallDropped("test")

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
	sCtx1, sCancel1 := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel1()
	cancel1(sCtx1)
	sCtx, sCancel = context.WithTimeout(context.Background(), defaultTestShortTimeout)
	defer sCancel()
	if _, err := lrsServer.LRSStreamCloseChan.Receive(sCtx); err != context.DeadlineExceeded {
		t.Fatal("LRS stream closed when expected to stay open")
	}

	// Cancel the second load reporting call, and ensure the stream is closed.
	sCtx2, sCancel2 := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel2()
	cancel2(sCtx2)
	if _, err := lrsServer.LRSStreamCloseChan.Receive(ctx); err != nil {
		t.Fatal("Timeout waiting for LRS stream to close")
	}

	// Calling the load reporting API again should result in the creation of a
	// new LRS stream. This ensures that creating and closing multiple streams
	// works smoothly.
	_, cancel3 := client.ReportLoad(serverConfig)
	if _, err := lrsServer.LRSStreamOpenChan.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for LRS stream to be created: %v", err)
	}
	sCtx3, sCancel3 := context.WithTimeout(ctx, defaultTestShortTimeout)
	defer sCancel3()
	cancel3(sCtx3)
}

// TestConcurrentReportLoad verifies that the client can safely handle concurrent
// requests to initiate load reporting streams. It launches multiple goroutines
// that all call client.ReportLoad simultaneously.
func (s) TestConcurrentReportLoad(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{SupportLoadReportingService: true})
	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
	client := createXDSClient(t, bc)

	serverConfig, err := bootstrap.ServerConfigForTesting(bootstrap.ServerConfigTestingOptions{URI: mgmtServer.Address})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}

	// Call ReportLoad() concurrently from multiple go routines.
	var wg sync.WaitGroup
	const numGoroutines = 10
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			_, cancelStore := client.ReportLoad(serverConfig)
			defer cancelStore(ctx)
		}()
	}
	wg.Wait()
}

// TestConcurrentChannels verifies that we can create multiple gRPC channels
// concurrently with a shared XDSClient, each of which will create a new LRS
// stream without any race.
func (s) TestConcurrentChannels(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{AllowResourceSubset: true, SupportLoadReportingService: true})

	nodeID := uuid.New().String()
	bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)

	if internal.NewXDSResolverWithPoolForTesting == nil {
		t.Fatalf("internal.NewXDSResolverWithConfigForTesting is nil")
	}

	config, err := bootstrap.NewConfigFromContents(bc)
	if err != nil {
		t.Fatalf("Failed to parse bootstrap contents: %s, %v", string(bc), err)
	}
	pool := xdsclient.NewPool(config)

	resolverBuilder := internal.NewXDSResolverWithPoolForTesting.(func(*xdsclient.Pool) (resolver.Builder, error))
	xdsResolver, err := resolverBuilder(pool)
	if err != nil {
		t.Fatalf("Failed to create xDS resolver for testing: %v", err)
	}

	server := stubserver.StartTestService(t, nil)
	defer server.Stop()

	// Configure the management server with resources that enable LRS.
	const serviceName = "my-service-e2e-lrs-test"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       testutils.ParsePort(t, server.Address),
		SecLevel:   e2e.SecurityLevelNone,
	})
	resources.Clusters[0].LrsServer = &v3corepb.ConfigSource{
		ConfigSourceSpecifier: &v3corepb.ConfigSource_Self{
			Self: &v3corepb.SelfConfigSource{},
		},
	}
	if err := mgmtServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const (
		numGoroutines = 10
		numRPCs       = 10
	)
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range numRPCs {
				cc, err := grpc.NewClient(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(xdsResolver))
				if err != nil {
					t.Errorf("grpc.NewClient() failed: %v", err)
					return
				}
				defer cc.Close()

				testClient := testgrpc.NewTestServiceClient(cc)
				if _, err := testClient.EmptyCall(ctx, &testpb.Empty{}); err != nil {
					t.Errorf("EmptyCall() failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
