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
 */

// Package transport_test contains e2e style tests for the xDS transport
// implementation. It uses the envoy-go-control-plane as the management server.
package transport_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/fakeserver"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	defaultTestTimeout      = 5 * time.Second
	defaultTestShortTimeout = 10 * time.Millisecond
)

// startFakeManagementServer starts a fake xDS management server and returns a
// cleanup function to close the fake server.
func startFakeManagementServer(t *testing.T) (*fakeserver.Server, func()) {
	t.Helper()
	fs, sCleanup, err := fakeserver.StartServer()
	if err != nil {
		t.Fatalf("Failed to start fake xDS server: %v", err)
	}
	return fs, sCleanup
}

// resourcesWithTypeURL wraps resources and type URL received from server.
type resourcesWithTypeURL struct {
	resources []*anypb.Any
	url       string
}

// TestHandleResponseFromManagementServer covers different scenarios of the
// transport receiving a response from the management server. In all scenarios,
// the trasport is expected to pass the received responses as-is to the data
// model layer for validation and not perform any validation on its own.
func (s) TestHandleResponseFromManagementServer(t *testing.T) {
	const (
		resourceName1 = "resource-name-1"
		resourceName2 = "resource-name-2"
	)
	var (
		badlyMarshaledResource = &anypb.Any{
			TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
			Value:   []byte{1, 2, 3, 4},
		}
		apiListener = &v3listenerpb.ApiListener{
			ApiListener: func() *anypb.Any {
				return testutils.MarshalAny(&v3httppb.HttpConnectionManager{
					RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
						Rds: &v3httppb.Rds{
							ConfigSource: &v3corepb.ConfigSource{
								ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
							},
							RouteConfigName: "route-configuration-name",
						},
					},
				})
			}(),
		}
		resource1 = &v3listenerpb.Listener{
			Name:        resourceName1,
			ApiListener: apiListener,
		}
		resource2 = &v3listenerpb.Listener{
			Name:        resourceName2,
			ApiListener: apiListener,
		}
	)

	tests := []struct {
		desc                     string
		resourceNamesToRequest   []string
		managementServerResponse *v3discoverypb.DiscoveryResponse
		wantURL                  string
		wantResources            []*anypb.Any
	}{
		{
			desc:                   "badly marshaled response",
			resourceNamesToRequest: []string{resourceName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				TypeUrl:   "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{badlyMarshaledResource},
			},
			wantURL:       "type.googleapis.com/envoy.config.listener.v3.Listener",
			wantResources: []*anypb.Any{badlyMarshaledResource},
		},
		{
			desc:                     "empty response",
			resourceNamesToRequest:   []string{resourceName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{},
			wantURL:                  "",
			wantResources:            nil,
		},
		{
			desc:                   "one good resource",
			resourceNamesToRequest: []string{resourceName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				TypeUrl:   "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{testutils.MarshalAny(resource1)},
			},
			wantURL:       "type.googleapis.com/envoy.config.listener.v3.Listener",
			wantResources: []*anypb.Any{testutils.MarshalAny(resource1)},
		},
		{
			desc:                   "two good resources",
			resourceNamesToRequest: []string{resourceName1, resourceName2},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				TypeUrl:   "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{testutils.MarshalAny(resource1), testutils.MarshalAny(resource2)},
			},
			wantURL:       "type.googleapis.com/envoy.config.listener.v3.Listener",
			wantResources: []*anypb.Any{testutils.MarshalAny(resource1), testutils.MarshalAny(resource2)},
		},
		{
			desc:                   "two resources when we requested one",
			resourceNamesToRequest: []string{resourceName1},
			managementServerResponse: &v3discoverypb.DiscoveryResponse{
				TypeUrl:   "type.googleapis.com/envoy.config.listener.v3.Listener",
				Resources: []*anypb.Any{testutils.MarshalAny(resource1), testutils.MarshalAny(resource2)},
			},
			wantURL:       "type.googleapis.com/envoy.config.listener.v3.Listener",
			wantResources: []*anypb.Any{testutils.MarshalAny(resource1), testutils.MarshalAny(resource2)},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Create a fake xDS management server listening on a local port,
			// and set it up with the response to send.
			mgmtServer, cleanup := startFakeManagementServer(t)
			defer cleanup()
			t.Logf("Started xDS management server on %s", mgmtServer.Address)
			mgmtServer.XDSResponseChan <- &fakeserver.Response{Resp: test.managementServerResponse}

			// Construct the server config to represent the management server.
			serverCfg := bootstrap.ServerConfig{
				ServerURI:    mgmtServer.Address,
				Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
				CredsType:    "insecure",
				TransportAPI: version.TransportV3,
				NodeProto:    &v3corepb.Node{Id: uuid.New().String()},
			}

			// Create a new transport.
			resourcesCh := testutils.NewChannel()
			tr, err := transport.New(transport.Options{
				ServerCfg: serverCfg,
				// No validation. Simply push received resources on a channel.
				UpdateHandler: func(update transport.ResourceUpdate) error {
					resourcesCh.Send(&resourcesWithTypeURL{
						resources: update.Resources,
						url:       update.URL,
						// Ignore resource version here.
					})
					return nil
				},
				StreamErrorHandler: func(error) {},                                      // No stream error handling.
				Backoff:            func(int) time.Duration { return time.Duration(0) }, // No backoff.
			})
			if err != nil {
				t.Fatalf("Failed to create xDS transport: %v", err)
			}
			defer tr.Close()

			// Send the request, and validate that the response sent by the
			// management server is propagated to the data model layer.
			tr.SendRequest(version.V3ListenerURL, test.resourceNamesToRequest)
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			v, err := resourcesCh.Receive(ctx)
			if err != nil {
				t.Fatalf("Failed to receive resources at the data model layer: %v", err)
			}
			gotURL := v.(*resourcesWithTypeURL).url
			gotResources := v.(*resourcesWithTypeURL).resources
			if gotURL != test.wantURL {
				t.Fatalf("Received resource URL in response: %s, want %s", gotURL, test.wantURL)
			}
			if diff := cmp.Diff(gotResources, test.wantResources, protocmp.Transform()); diff != "" {
				t.Fatalf("Received unexpected resources. Diff (-got, +want):\n%s", diff)
			}
		})
	}
}
