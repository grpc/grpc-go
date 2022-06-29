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

const (
	// We are interested in LDS, RDS, CDS and EDS resources as part of the regular
	// xDS flow on the client.
	wantResources = 4
	// Management server assigns monotonically increasing stream IDs starting at 1.
	idBeforeRestart = 1
	idAfterRestart  = 2
)

type streamRequestTuple struct {
	id  int64                           // Assigned by the management server.
	req *v3discoverypb.DiscoveryRequest // As received by the managerment server.
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

	// Event to notify stream closure.
	streamClosed := grpcsync.NewEvent()
	// ADS requests received by the management server are pushed on to this
	// channel from the below registered callbacks. A big buffer is configured
	// on the channel to accommodate for the fact that at least two requests are
	// required for every resource (the initial one and the ACK), and because it
	// allows us to have simpler code.
	requestsCh := make(chan streamRequestTuple, 50)
	managementServer, nodeID, _, resolver, cleanup1 := e2e.SetupManagementServer(t, &e2e.ManagementServerOptions{
		Listener: lis,
		OnStreamRequest: func(id int64, req *v3discoverypb.DiscoveryRequest) error {
			requestsCh <- streamRequestTuple{id: id, req: req}
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

	// Wait for all the resources to be ACKed.
	ackVersionsBeforeRestart := make(map[string]string)
AcksBeforeRestart:
	for {
		select {
		case tuple := <-requestsCh:
			if tuple.id != idBeforeRestart {
				t.Fatalf("Received request with stream ID %d, expecting %d", tuple.id, idBeforeRestart)
			}
			// The client first requests for a resource with version set to empty
			// string. After receipt of the response, it sends another request for
			// the same resource, this time with a non-empty version string. This
			// corresponds to ACKs, and this is what we want to capture.
			req := tuple.req
			if len(req.GetResourceNames()) != 0 && req.GetVersionInfo() != "" {
				ackVersionsBeforeRestart[req.GetTypeUrl()] = req.GetVersionInfo()
			}
			if len(ackVersionsBeforeRestart) == wantResources {
				break AcksBeforeRestart
			}
		case <-ctx.Done():
			t.Fatal("timeout when waiting for resources to be requested before stream restart")
		}
	}

	// Stop the listener on the management server. This will cause the client to
	// backoff and recreate the stream.
	lis.Stop()

	// Wait for the stream to be closed on the server.
	<-streamClosed.Done()

	// Restart the listener on the management server to be able to accept
	// reconnect attempts from the client.
	lis.Restart()

	// Wait for all the previously sent resources to be re-requested.
	ackVersionsAfterRestart := make(map[string]string)
AcksAfterRestart:
	for {
		select {
		case tuple := <-requestsCh:
			if tuple.id == idBeforeRestart {
				// Ignore any stray requests from the old stream.
				continue
			}
			if tuple.id != idAfterRestart {
				t.Fatalf("Received request with stream ID %d, expecting %d", tuple.id, idAfterRestart)
			}
			// After stream closure, capture the first request for every resource.
			// This should not be set to an empty version string, but instead should
			// be set to the version last ACKed before stream closure.
			req := tuple.req
			if len(req.GetResourceNames()) != 0 {
				ackVersionsAfterRestart[req.GetTypeUrl()] = req.GetVersionInfo()
			}
			if len(ackVersionsAfterRestart) == wantResources {
				break AcksAfterRestart
			}
		case <-ctx.Done():
			t.Fatal("timeout when waiting for resources to be re-requested after stream restart")
		}
	}

	if !cmp.Equal(ackVersionsBeforeRestart, ackVersionsAfterRestart) {
		t.Fatalf("ackVersionsBeforeRestart: %v and ackVersionsAfterRestart: %v don't match", ackVersionsBeforeRestart, ackVersionsAfterRestart)
	}
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}
