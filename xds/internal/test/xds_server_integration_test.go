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
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	basepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v2"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/xds"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/version"
)

const (
	defaultTestTimeout = 10 * time.Second
	localAddress       = "127.0.0.1:9999"
	listenerName       = "grpc/server?udpa.resource.listening_address=127.0.0.1:9999"
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
	// Spin up a fake xDS management server on a local port.
	// TODO(easwars): Switch to using the server from envoy-go-control-plane.
	fs, cleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("failed to start fake xDS server: %v", err)
	}
	defer cleanup()
	t.Logf("Started xDS management server at %s", fs.Address)

	// Setup the fakeserver to respond with a Listener resource.
	fs.XDSResponseChan <- &fakeserver.Response{
		Resp: &xdspb.DiscoveryResponse{
			Resources: []*anypb.Any{
				{
					TypeUrl: version.V2ListenerURL,
					Value: func() []byte {
						l := &xdspb.Listener{
							// This needs to match the name we are querying for.
							Name: listenerName,
							ApiListener: &listenerpb.ApiListener{
								ApiListener: &anypb.Any{
									TypeUrl: version.V2HTTPConnManagerURL,
									Value: func() []byte {
										cm := &httppb.HttpConnectionManager{
											RouteSpecifier: &httppb.HttpConnectionManager_Rds{
												Rds: &httppb.Rds{
													ConfigSource: &basepb.ConfigSource{
														ConfigSourceSpecifier: &basepb.ConfigSource_Ads{Ads: &basepb.AggregatedConfigSource{}},
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
						ml, _ := proto.Marshal(l)
						return ml
					}(),
				},
			},
			TypeUrl: version.V2ListenerURL,
		},
	}

	// Create a bootstrap file in a temporary directory.
	tmpdir, err := ioutil.TempDir("", "xds-server-test*")
	if err != nil {
		t.Fatalf("failed to create tempdir: %v", err)
	}
	bootstrapContents := fmt.Sprintf(`
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "%s",
				"channel_creds": [
					{ "type": "insecure" }
				]
			}]
		}`, fs.Address)
	bootstrapFileName := path.Join(tmpdir, "bootstrap")
	if err := ioutil.WriteFile(bootstrapFileName, []byte(bootstrapContents), os.ModePerm); err != nil {
		t.Fatalf("failed to write bootstrap file: %v", err)
	}
	if err := os.Setenv("GRPC_XDS_BOOTSTRAP", bootstrapFileName); err != nil {
		t.Fatalf("failed to update bootstrap environment variable: %v", err)
	}
	t.Logf("Create bootstrap file at %s with contents\n%s", bootstrapFileName, bootstrapContents)

	// Initialize a gRPC server which uses xDS, and register stubServer on it.
	server := xds.NewGRPCServer()
	testpb.RegisterTestServiceServer(server, &testService{})
	defer server.Stop()

	go server.Serve(xds.ServeOptions{Network: "tcp", Address: localAddress})

	// Create a clientconn and make a successful RPC
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx, localAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
