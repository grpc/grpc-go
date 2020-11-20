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

package v3

import (
	"context"
	"fmt"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3discoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/e2e"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Configure management server with callbacks.
//   - Also, each test should be able to configure the response callback so that error responses can be injected.
// Create versioned api client talking to management server
// Make sure
//  - ADS stream is created
//  - request is sent with appropriate resource names
//  - compare the expected update
//  - make sure ack is sent if a good response was received.
func (s) TestLDSHandleResponse(t *testing.T) {
	tests := []struct {
		desc              string
		watchResourceName string
		overrideResources []*anypb.Any
		wantUpdate        *xdsclient.ListenerUpdate
	}{
		{
			desc:              "badly-marshaled-response",
			watchResourceName: listenerName,
			overrideResources: []*anypb.Any{
				{
					TypeUrl: version.V3ListenerURL,
					Value:   []byte{1, 2, 3, 4},
				},
			},
		},
		{
			desc:              "no-listener-proto-in-response",
			watchResourceName: listenerName,
			overrideResources: []*anypb.Any{
				{
					TypeUrl: version.V2HTTPConnManagerURL,
					Value: func() []byte {
						cm := &v3httppb.HttpConnectionManager{
							RouteSpecifier: &v3httppb.HttpConnectionManager_Rds{
								Rds: &v3httppb.Rds{
									ConfigSource: &v3corepb.ConfigSource{
										ConfigSourceSpecifier: &v3corepb.ConfigSource_Ads{Ads: &v3corepb.AggregatedConfigSource{}},
									},
									RouteConfigName: routeName,
								},
							},
						}
						mcm, _ := proto.Marshal(cm)
						return mcm
					}(),
				},
			},
		},
		{
			desc:              "no-apiListener-in-response",
			watchResourceName: listenerName,
			overrideResources: []*anypb.Any{
				{
					TypeUrl: version.V2ListenerURL,
					Value: func() []byte {
						l := &v3listenerpb.Listener{Name: listenerName}
						ml, _ := proto.Marshal(l)
						return ml
					}(),
				},
			},
		},
		{
			desc:              "one-good-listener",
			watchResourceName: listenerName,
			wantUpdate:        &xdsclient.ListenerUpdate{RouteConfigName: routeName},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Start the xDS management server with callbacks to notify the test about
			// important events.
			streamOpenCh := testutils.NewChannel()
			streamRequestCh := testutils.NewChannel()
			versionCh := testutils.NewChannel()
			cb := &e2e.CallbackFuncs{
				StreamOpenFunc: func(context.Context, int64, string) error {
					streamOpenCh.Send(nil)
					return nil
				},
				StreamRequestFunc: func(_ int64, req *v3discoverypb.DiscoveryRequest) error {
					streamRequestCh.Send(req)
					return nil
				},
				StreamResponseFunc: func(_ int64, _ *v3discoverypb.DiscoveryRequest, resp *v3discoverypb.DiscoveryResponse) {
					if test.overrideResources != nil {
						resp.Resources = test.overrideResources
					}
					versionCh.Send(resp.GetVersionInfo())
				},
			}
			mgmtServer, err := e2e.StartManagementServer(cb)
			if err != nil {
				t.Fatal(err)
			}
			defer mgmtServer.Stop()

			// Configure the management server with a set of listener resources.
			nodeID := uuid.New().String()
			if err := mgmtServer.Update(e2e.UpdateOptions{
				NodeID:    nodeID,
				Listeners: []*v3listenerpb.Listener{listener},
			}); err != nil {
				t.Fatal(err)
			}

			// Dial the management server.
			cc, err := grpc.Dial(mgmtServer.Address, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatalf("Failed to dial management server: %v", err)
			}
			defer cc.Close()

			// Create a v3Client with a fake parent update receiver which simply
			// pushes the update on a channel for the test so inspect.
			gotUpdateCh := testutils.NewChannel()
			v3c, err := newV3Client(&testUpdateReceiver{
				f: func(rType xdsclient.ResourceType, d map[string]interface{}) {
					u, ok := d[listenerName]
					if !ok {
						t.Errorf("received unexpected updated: %+v", d)
					}
					gotUpdateCh.Send(u)
				},
			}, cc, &v3corepb.Node{Id: nodeID}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer v3c.Close()
			t.Log("Started xds v3Client...")

			// Make sure an ADS stream is opened to our management server.
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err := streamOpenCh.Receive(ctx); err != nil {
				t.Fatalf("timeout when waiting for an ADS stream to be opened: %v", err)
			}

			// Register a watch on the v3Client for an LDS resource.
			v3c.AddWatch(xdsclient.ListenerResource, test.watchResourceName)

			// Make sure a discovery request is sent with the exact resource
			// name that we registered the watch for.
			r, err := streamRequestCh.Receive(ctx)
			if err != nil {
				t.Fatalf("timeout when waiting for a Discovery request to be sent: %v", err)
			}
			req := r.(*v3discoverypb.DiscoveryRequest)
			wantNames := []string{test.watchResourceName}
			gotNames := req.GetResourceNames()
			if !cmp.Equal(gotNames, wantNames) {
				t.Fatalf("resource_names in discovery request is %v, want %v", gotNames, wantNames)
			}

			// If an update was expected from the v3Client, make sure we get the
			// expected update.
			if test.wantUpdate != nil {
				if err := waitForListenerUpdate(ctx, gotUpdateCh, *test.wantUpdate); err != nil {
					t.Fatal(err)
				}
			}

			// If an update was expected, we should see an ACK being sent out.
			// Otherwise, we should see a NACK.
			if err := waitForACKorNACK(ctx, streamRequestCh, versionCh, test.wantUpdate != nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func waitForACKorNACK(ctx context.Context, reqCh, versionCh *testutils.Channel, ack bool) error {
	// Grab the version of the resource sent by the management server.
	v, err := versionCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for version info in response: %v", err)
	}
	version := v.(string)

	// Grab the version in the ack/nack sent by the client.
	r, err := reqCh.Receive(ctx)
	if err != nil {
		return fmt.Errorf("timeout when waiting for a ACK/NACK to be sent: %v", err)
	}
	req := r.(*v3discoverypb.DiscoveryRequest)

	// If we are expecting an ACK, make sure the expected version is populated.
	if ack {
		if req.GetVersionInfo() != version {
			return fmt.Errorf("got ACK with version: %v, want: %v", req.GetVersionInfo(), version)
		}
		return nil
	}
	// Else, this is the NACK case. Version should be empty here.
	if req.GetVersionInfo() != "" {
		return fmt.Errorf("got ACK with version: %v, want a NACK with no version", req.GetVersionInfo())
	}
	return nil
}
