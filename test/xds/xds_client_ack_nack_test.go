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

package xds_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"

	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	testgrpc "google.golang.org/grpc/test/grpc_testing"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// We are interested in LDS, RDS, CDS and EDS resources as part of the regular
// xDS flow on the client.
const wantResources = 4

// seenAllACKs returns true if we have seen two streams with acks for all the
// resources that we are interested in.
func seenAllACKs(acks map[int64]map[string]string) bool {
	if len(acks) != 2 {
		return false
	}
	for _, v := range acks {
		if len(v) != wantResources {
			return false
		}
	}
	return true
}

// TestClientResourceVersionAfterStreamRestart tests the scenario where the
// xdsClient's ADS stream to the management server gets broken. This test
// verifies that the version number on the initial request on the new stream
// indicates the most recent version seen by the client on the previous stream.
func (s) TestClientResourceVersionAfterStreamRestart(t *testing.T) {
	// Create a restartable listener which can close existing connections.
	l, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}
	lis := testutils.NewRestartableListener(l)

	streamClosed := grpcsync.NewEvent() // Event to notify stream closure.
	acksReceived := grpcsync.NewEvent() // Event to notify receipt of acks for all resources.
	// Map from stream id to a map of resource type to resource version.
	ackVersionsMap := make(map[int64]map[string]string)
	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t, &e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			// Return early under the following circumstances:
			// - Received all the requests we wanted to see. This is to avoid
			//   any stray requests leading to test flakes.
			// - Request contains no resource names. Such requests are usually
			//   seen when the xdsclient is shutting down and is no longer
			//   interested in the resources that it had subscribed to earlier.
			if acksReceived.HasFired() || len(req.GetResourceNames()) == 0 {
				return nil
			}
			// Create a stream specific map to store ack versions if this is the
			// first time we are seeing this stream id.
			if ackVersionsMap[id] == nil {
				ackVersionsMap[id] = make(map[string]string)
			}
			ackVersionsMap[id][req.GetTypeUrl()] = req.GetVersionInfo()
			if seenAllACKs(ackVersionsMap) {
				acksReceived.Fire()
			}
			return nil
		},
		OnStreamClosed: func(int64) {
			streamClosed.Fire()
		},
	})
	defer cleanup1()

	port, cleanup2 := startTestService(t, nil)
	defer cleanup2()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testgrpc.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}

	// A successful RPC means that we have captured the ack versions for all
	// resources in the OnStreamRequest callback. Nothing more needs to be done
	// here before stream restart.

	// Stop the listener on the management server. This will cause the client to
	// backoff and recreate the stream.
	lis.Stop()

	// Wait for the stream to be closed on the server.
	<-streamClosed.Done()

	// Restart the listener on the management server to be able to accept
	// reconnect attempts from the client.
	lis.Restart()

	// Wait for all the previously sent resources to be re-requested.
	<-acksReceived.Done()

	// We depend on the fact that the management server assigns monotonically
	// increasing stream IDs starting at 1.
	const (
		idBeforeRestart = 1
		idAfterRestart  = 2
	)
	if diff := cmp.Diff(ackVersionsMap[idBeforeRestart], ackVersionsMap[idAfterRestart]); diff != "" {
		t.Fatalf("unexpected diff in ack versions before and after stream restart (-want, +got):\n%s", diff)
	}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
