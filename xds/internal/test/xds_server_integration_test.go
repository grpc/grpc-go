// +build !386

/*
 *
 * Copyright 2020 gRPC authors.
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

// Package xds_test contains e2e tests for xDS use on the server.
package xds_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/version"
)

const (
	defaultTestTimeout = 10 * time.Second
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestServerSideXDS is an e2e tests for xDS use on the server. This does not
// use any xDS features because we have not implemented any on the server side.
func (s) TestServerSideXDS(t *testing.T) {
	origV3Support := env.V3Support
	env.V3Support = true
	defer func() { env.V3Support = origV3Support }()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	// Spin up a xDS management server on a local port.
	nodeID := uuid.New().String()
	fs, err := e2e.StartManagementServer()
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Stop()

	// Create a bootstrap file in a temporary directory.
	cleanup, err := e2e.SetupBootstrapFile(e2e.BootstrapOptions{
		Version:   e2e.TransportV3,
		NodeID:    nodeID,
		ServerURI: fs.Address,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Initialize an xDS-enabled gRPC server and register the stubServer on it.
	server := xds.NewGRPCServer()
	testpb.RegisterTestServiceServer(server, &testService{})
	defer server.Stop()

	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	// Setup the fake management server to respond with a Listener resource.
	go func() {
		listener := &v3listenerpb.Listener{
			// This needs to match the name we are querying for.
			Name: fmt.Sprintf("grpc/server?udpa.resource.listening_address=%s", lis.Addr().String()),
			ApiListener: &v3listenerpb.ApiListener{
				ApiListener: &anypb.Any{
					TypeUrl: version.V2HTTPConnManagerURL,
					Value: func() []byte {
						cm := &v3httppb.HttpConnectionManager{
							RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
								Rds: &v3httppb.Rds{
									ConfigSource: &v3corepb.ConfigSource{
										ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
									},
									RouteConfigName: "route-config",
								},
							},
						}
						mcm, _ := proto.Marshal(cm)
						return mcm
					}(),
				},
			},
		}
		if err := fs.Update(e2e.UpdateOptions{
			NodeID:    nodeID,
			Listeners: []*v3listenerpb.Listener{listener},
		}); err != nil {
			t.Error(err)
		}
	}()

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

type testService struct {
	testpb.TestServiceServer
}

func (*testService) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	return &testpb.Empty{}, nil
}
