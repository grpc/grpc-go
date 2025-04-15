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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/grpc/xds/internal/clients/grpctransport"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/clients/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/clients/lrsclient"
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
	testLocality1                 = `{"region":"test-region1"}`
	testLocality2                 = `{"region":"test-region2"}`
	testKey1                      = "test-key1"
	testKey2                      = "test-key2"
	defaultTestWatchExpiryTimeout = 100 * time.Millisecond
	defaultTestTimeout            = 5 * time.Second
	defaultTestShortTimeout       = 10 * time.Millisecond // For events expected to *not* happen.
)

var (
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

	credentials := map[string]credentials.Bundle{"insecure": insecure.NewBundle()}
	config := lrsclient.Config{
		Node:             clients.Node{ID: nodeID, UserAgentName: "user-agent", UserAgentVersion: "0.0.0.0"},
		TransportBuilder: grpctransport.NewBuilder(credentials),
	}
	client, err := lrsclient.New(config)
	if err != nil {
		t.Fatalf("lrsclient.New() failed: %v", err)
	}

	serverIdentifier1 := clients.ServerIdentifier{ServerURI: mgmtServer1.Address, Extensions: grpctransport.ServerIdentifierExtension{Credentials: "insecure"}}
	loadStore1, err := client.ReportLoad(serverIdentifier1)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer loadStore1.Stop()

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
	serverIdentifier2 := clients.ServerIdentifier{ServerURI: mgmtServer2.Address, Extensions: grpctransport.ServerIdentifierExtension{Credentials: "insecure"}}
	loadStore2, err := client.ReportLoad(serverIdentifier2)
	if err != nil {
		t.Fatalf("client.ReportLoad() failed: %v", err)
	}
	defer loadStore2.Stop()
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

	// Cancel this load reporting stream, server should see error canceled.
	loadStore2.Stop()

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
