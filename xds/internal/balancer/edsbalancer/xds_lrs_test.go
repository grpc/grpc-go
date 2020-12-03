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
 *
 */

package edsbalancer

import (
	"context"
	"testing"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/xds/internal/testutils/fakeclient"
)

// TestXDSLoadReporting verifies that the edsBalancer starts the loadReport
// stream when the lbConfig passed to it contains a valid value for the LRS
// server (empty string).
func (s) TestXDSLoadReporting(t *testing.T) {
	xdsC := fakeclient.NewClient()
	oldNewXDSClient := newXDSClient
	newXDSClient = func() (xdsClientInterface, error) { return xdsC, nil }
	defer func() { newXDSClient = oldNewXDSClient }()

	builder := balancer.Get(edsName)
	edsB := builder.Build(newNoopTestClientConn(), balancer.BuildOptions{})
	if edsB == nil {
		t.Fatalf("builder.Build(%s) failed and returned nil", edsName)
	}
	defer edsB.Close()

	if err := edsB.UpdateClientConnState(balancer.ClientConnState{
		BalancerConfig: &EDSConfig{
			EDSServiceName:             testEDSClusterName,
			LrsLoadReportingServerName: new(string),
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotCluster, err := xdsC.WaitForWatchEDS(ctx)
	if err != nil {
		t.Fatalf("xdsClient.WatchEndpoints failed with error: %v", err)
	}
	if gotCluster != testEDSClusterName {
		t.Fatalf("xdsClient.WatchEndpoints() called with cluster: %v, want %v", gotCluster, testEDSClusterName)
	}

	got, err := xdsC.WaitForReportLoad(ctx)
	if err != nil {
		t.Fatalf("xdsClient.ReportLoad failed with error: %v", err)
	}
	if got.Server != "" {
		t.Fatalf("xdsClient.ReportLoad called with {%v}: want {\"\"}", got.Server)
	}
}
