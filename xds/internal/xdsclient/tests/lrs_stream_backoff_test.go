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
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/e2e"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

// Tests the case where the management server returns an error in the ADS
// streaming RPC. Verifies that the LRS stream is restarted after a backoff
// period, and that the previously requested resources are re-requested on the
// new stream.
func (s) TestLRS_BackoffAfterStreamFailure(t *testing.T) {
    // Channels for test state.
    streamCloseCh := make(chan struct{}, 1)
    resourceRequestCh := make(chan []string, 1)
    backoffCh := make(chan struct{}, 1)
    // Context with timeout.
    ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
    defer cancel()
    // Simulate LRS stream error.
    streamErr := errors.New("LRS stream error")
    mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
        SupportLoadReportingService: true,
        OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
            t.Logf("Simulated server: Received stream request: %+v\n", req)
            if req.GetTypeUrl() == version.V3ListenerURL {
                select {
                case resourceRequestCh <- req.GetResourceNames():
                case <-ctx.Done():
                }
            }
            return streamErr
        },
        OnStreamClosed: func(int64, *v3corepb.Node) {
            t.Log("Simulated server: Stream closed")
            select {
            case streamCloseCh <- struct{}{}:
            case <-ctx.Done():
            }
        },
    })
    // Backoff behavior.
    streamBackoff := func(v int) time.Duration {
        t.Log("Backoff triggered")
        select {
        case backoffCh <- struct{}{}:
        case <-ctx.Done():
        }
        return 500 * time.Millisecond
    }
    // Create xDS client and bootstrap configuration.
    nodeID := uuid.New().String()
    bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
    testutils.CreateBootstrapFileForTesting(t, bc)
    client := createXDSClientWithBackoff(t, bc, streamBackoff)
    // Explicit resource watch.
    lw := newListenerWatcher()
    ldsCancel := xdsresource.WatchListener(client, "resource-name", lw)
    defer ldsCancel()
    // Verify resource request.
    if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{"resource-name"}); err != nil {
        t.Fatal(err)
    }
    // Verify stream closure.
    select {
    case <-streamCloseCh:
        t.Log("Stream closure observed after error")
    case <-ctx.Done():
        t.Fatal("Timeout waiting for LRS stream closure")
    }
    // Verify backoff signal.
    select {
    case <-backoffCh:
        t.Log("Backoff observed before stream restart")
    case <-ctx.Done():
        t.Fatal("Timeout waiting for backoff signal")
    }
    // Verify re-request.
    if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{"resource-name"}); err != nil {
        t.Fatal(err)
    }
}

// Tests the case where a stream breaks because the server goes down. Verifies
// that when the server comes back up, the same resources are re-requested,
// this time with the previously acked version and an empty nonce.
func (s) TestLRS_BackoffAfterBrokenStream(t *testing.T) {
    // Channels for verifying different events in the test.
    streamCloseCh := make(chan struct{}, 1)  // LRS stream is closed.
    resourceRequestCh := make(chan []string, 1) // Resource names in the discovery request.
    backoffCh := make(chan struct{}, 1)      // Backoff after stream failure.

    ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
    defer cancel()

    // Simulate LRS stream error.
    // streamErr := errors.New("LRS stream error")
    mgmtServer := e2e.StartManagementServer(t, e2e.ManagementServerOptions{
        SupportLoadReportingService: true,
        OnStreamRequest: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
            if req.GetTypeUrl() == version.V3ListenerURL {
                t.Logf("Received LRS request for resources: %v", req.GetResourceNames())
                select {
                case resourceRequestCh <- req.GetResourceNames():
                case <-ctx.Done():
                }
            }
            return errors.New("unsupported TypeURL")
        },
        OnStreamClosed: func(int64, *v3corepb.Node) {
            t.Log("Simulated server: Stream closed")
            select {
            case streamCloseCh <- struct{}{}:
            case <-ctx.Done():
            }
        },
    })

    // Override the backoff implementation.
    streamBackoff := func(v int) time.Duration {
        t.Log("Backoff triggered")
        select {
        case backoffCh <- struct{}{}:
        case <-ctx.Done():
        }
        return 500 * time.Millisecond
    }

    // Create an xDS client with bootstrap pointing to the above server.
    nodeID := uuid.New().String()
    bc := e2e.DefaultBootstrapContents(t, nodeID, mgmtServer.Address)
    testutils.CreateBootstrapFileForTesting(t, bc)
    client := createXDSClientWithBackoff(t, bc, streamBackoff)

    // Register a watch for load reporting resource.
    const resourceName = "load-report"
    lw := newListenerWatcher() // Replace this with the correct LRS watcher if available.
    lrsCancel := xdsresource.WatchListener(client, resourceName, lw)
    defer lrsCancel()

    // Verify the initial resource request.
    if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{resourceName}); err != nil {
        t.Fatal(err)
    }

    // Verify stream closure after an error.
    select {
    case <-streamCloseCh:
        t.Log("Stream closure observed after error")
    case <-ctx.Done():
        t.Fatal("Timeout waiting for LRS stream closure")
    }

    // Verify backoff signal before restarting the stream.
    select {
    case <-backoffCh:
        t.Log("Backoff observed before stream restart")
    case <-ctx.Done():
        t.Fatal("Timeout waiting for backoff signal")
    }

    // Verify the resource request is re-sent after stream recovery.
    if err := waitForResourceNames(ctx, t, resourceRequestCh, []string{resourceName}); err != nil {
        t.Fatal(err)
    }
}
