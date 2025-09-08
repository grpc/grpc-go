/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/testutils/stats"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource/version"
	"google.golang.org/protobuf/testing/protocmp"
)

const (
	testXDSServerURL    = "xds.example.com:8080"
	testXDSServerURL2   = "xds.example.com:8081"
	testNodeID          = "test-node-id"
	testClusterName     = "test-cluster"
	testUserAgentName   = "test-ua-name"
	testUserAgentVer    = "test-ua-ver"
	testLocalityRegion  = "test-region"
	testLocalityZone    = "test-zone"
	testLocalitySubZone = "test-sub-zone"
	testTargetName      = "test-target"
)

var (
	testMetadataJSON, _ = json.Marshal(map[string]any{"foo": "bar", "baz": float64(1)})
)

func (s) TestBuildXDSClientConfig_Success(t *testing.T) {
	tests := []struct {
		name                string
		bootstrapContents   []byte
		wantXDSClientConfig func(bootstrapCfg *bootstrap.Config) xdsclient.Config
	}{
		{
			name: "without authorities",
			bootstrapContents: []byte(fmt.Sprintf(`{
				"xds_servers": [{"server_uri": "%s", "channel_creds": [{"type": "insecure"}]}],
				"node": {
					"id": "%s", "cluster": "%s", "metadata": %s,
					"locality": {"region": "%s", "zone": "%s", "sub_zone": "%s"},
					"user_agent_name": "%s", "user_agent_version": "%s"
				}
			}`, testXDSServerURL, testNodeID, testClusterName, testMetadataJSON, testLocalityRegion, testLocalityZone, testLocalitySubZone, testUserAgentName, testUserAgentVer)),
			wantXDSClientConfig: func(c *bootstrap.Config) xdsclient.Config {
				node, serverCfg := c.Node(), c.XDSServers()[0]
				expectedServer := xdsclient.ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: serverCfg.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}}
				gServerCfgMap := map[xdsclient.ServerConfig]*bootstrap.ServerConfig{expectedServer: serverCfg}
				return xdsclient.Config{
					Servers:     []xdsclient.ServerConfig{expectedServer},
					Node:        clients.Node{ID: node.GetId(), Cluster: node.GetCluster(), Metadata: node.Metadata, Locality: clients.Locality{Region: node.Locality.Region, Zone: node.Locality.Zone, SubZone: node.Locality.SubZone}, UserAgentName: node.UserAgentName, UserAgentVersion: node.GetUserAgentVersion()},
					Authorities: map[string]xdsclient.Authority{},
					ResourceTypes: map[string]xdsclient.ResourceType{
						version.V3ListenerURL:    {TypeURL: version.V3ListenerURL, TypeName: xdsresource.ListenerResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericListenerResourceTypeDecoder(c)},
						version.V3RouteConfigURL: {TypeURL: version.V3RouteConfigURL, TypeName: xdsresource.RouteConfigTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericRouteConfigResourceTypeDecoder()},
						version.V3ClusterURL:     {TypeURL: version.V3ClusterURL, TypeName: xdsresource.ClusterResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericClusterResourceTypeDecoder(c, gServerCfgMap)},
						version.V3EndpointsURL:   {TypeURL: version.V3EndpointsURL, TypeName: xdsresource.EndpointsResourceTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericEndpointsResourceTypeDecoder()},
					},
					MetricsReporter: &metricsReporter{recorder: stats.NewTestMetricsRecorder(), target: testTargetName},
					TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
						"insecure": {
							Credentials: insecure.NewBundle(),
							GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
								opts = append(opts, serverCfg.DialOptions()...)
								return grpc.NewClient(target, opts...)
							}},
					}),
				}
			},
		},
		{
			name: "with authorities",
			bootstrapContents: []byte(fmt.Sprintf(`{
				"xds_servers": [{"server_uri": "%s", "channel_creds": [{"type": "insecure"}]}],
				"node": {"id": "%s"},
				"authorities": {
					"auth1": {},
					"auth2": {"xds_servers": [{"server_uri": "%s", "channel_creds": [{"type": "insecure"}]}]}
				}
			}`, testXDSServerURL, testNodeID, testXDSServerURL2)),
			wantXDSClientConfig: func(c *bootstrap.Config) xdsclient.Config {
				node := c.Node()
				topLevelSCfg, auth2SCfg := c.XDSServers()[0], c.Authorities()["auth2"].XDSServers[0]
				expTopLevelS := xdsclient.ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: topLevelSCfg.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}}
				expAuth2S := xdsclient.ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: auth2SCfg.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}}
				gSCfgMap := map[xdsclient.ServerConfig]*bootstrap.ServerConfig{expTopLevelS: topLevelSCfg, expAuth2S: auth2SCfg}
				return xdsclient.Config{
					Servers:     []xdsclient.ServerConfig{expTopLevelS},
					Node:        clients.Node{ID: node.GetId(), Cluster: node.GetCluster(), Metadata: node.Metadata, UserAgentName: node.UserAgentName, UserAgentVersion: node.GetUserAgentVersion()},
					Authorities: map[string]xdsclient.Authority{"auth1": {XDSServers: []xdsclient.ServerConfig{expTopLevelS}}, "auth2": {XDSServers: []xdsclient.ServerConfig{expAuth2S}}},
					ResourceTypes: map[string]xdsclient.ResourceType{
						version.V3ListenerURL:    {TypeURL: version.V3ListenerURL, TypeName: xdsresource.ListenerResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericListenerResourceTypeDecoder(c)},
						version.V3RouteConfigURL: {TypeURL: version.V3RouteConfigURL, TypeName: xdsresource.RouteConfigTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericRouteConfigResourceTypeDecoder()},
						version.V3ClusterURL:     {TypeURL: version.V3ClusterURL, TypeName: xdsresource.ClusterResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericClusterResourceTypeDecoder(c, gSCfgMap)},
						version.V3EndpointsURL:   {TypeURL: version.V3EndpointsURL, TypeName: xdsresource.EndpointsResourceTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericEndpointsResourceTypeDecoder()},
					},
					MetricsReporter: &metricsReporter{recorder: stats.NewTestMetricsRecorder(), target: testTargetName},
					TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
						"insecure": {
							Credentials: insecure.NewBundle(),
							GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
								opts = append(opts, topLevelSCfg.DialOptions()...)
								return grpc.NewClient(target, opts...)
							}},
					}),
				}
			},
		},
		{
			name: "server features with ignore_resource_deletion",
			bootstrapContents: []byte(fmt.Sprintf(`{
				"xds_servers": [{"server_uri": "%s", "channel_creds": [{"type": "insecure"}], "server_features": ["ignore_resource_deletion"]}],
				"node": {"id": "%s"}
			}`, testXDSServerURL, testNodeID)),
			wantXDSClientConfig: func(c *bootstrap.Config) xdsclient.Config {
				node, serverCfg := c.Node(), c.XDSServers()[0]
				expectedServer := xdsclient.ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: serverCfg.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}, IgnoreResourceDeletion: true}
				gServerCfgMap := map[xdsclient.ServerConfig]*bootstrap.ServerConfig{expectedServer: serverCfg}
				return xdsclient.Config{
					Servers:     []xdsclient.ServerConfig{expectedServer},
					Node:        clients.Node{ID: node.GetId(), Cluster: node.GetCluster(), Metadata: node.Metadata, UserAgentName: node.UserAgentName, UserAgentVersion: node.GetUserAgentVersion()},
					Authorities: map[string]xdsclient.Authority{},
					ResourceTypes: map[string]xdsclient.ResourceType{
						version.V3ListenerURL:    {TypeURL: version.V3ListenerURL, TypeName: xdsresource.ListenerResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericListenerResourceTypeDecoder(c)},
						version.V3RouteConfigURL: {TypeURL: version.V3RouteConfigURL, TypeName: xdsresource.RouteConfigTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericRouteConfigResourceTypeDecoder()},
						version.V3ClusterURL:     {TypeURL: version.V3ClusterURL, TypeName: xdsresource.ClusterResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericClusterResourceTypeDecoder(c, gServerCfgMap)},
						version.V3EndpointsURL:   {TypeURL: version.V3EndpointsURL, TypeName: xdsresource.EndpointsResourceTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericEndpointsResourceTypeDecoder()},
					},
					MetricsReporter: &metricsReporter{recorder: stats.NewTestMetricsRecorder(), target: testTargetName},
					TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
						"insecure": {
							Credentials: insecure.NewBundle(),
							GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
								opts = append(opts, serverCfg.DialOptions()...)
								return grpc.NewClient(target, opts...)
							}},
					}),
				}
			},
		},
		{
			name: "channel creds - unknown type skipped",
			bootstrapContents: []byte(fmt.Sprintf(`{
				"xds_servers": [{"server_uri": "%s", "channel_creds": [{"type": "unknown-type"}, {"type": "insecure"}]}],
				"node": {"id": "%s"}
			}`, testXDSServerURL, testNodeID)), // "insecure" is selected
			wantXDSClientConfig: func(c *bootstrap.Config) xdsclient.Config {
				node, serverCfg := c.Node(), c.XDSServers()[0] // SelectedCreds will be "insecure"
				expectedServer := xdsclient.ServerConfig{ServerIdentifier: clients.ServerIdentifier{ServerURI: serverCfg.ServerURI(), Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "insecure"}}}
				gServerCfgMap := map[xdsclient.ServerConfig]*bootstrap.ServerConfig{expectedServer: serverCfg}
				return xdsclient.Config{
					Servers:     []xdsclient.ServerConfig{expectedServer},
					Node:        clients.Node{ID: node.GetId(), Cluster: node.GetCluster(), Metadata: node.Metadata, UserAgentName: node.UserAgentName, UserAgentVersion: node.GetUserAgentVersion()},
					Authorities: map[string]xdsclient.Authority{},
					ResourceTypes: map[string]xdsclient.ResourceType{
						version.V3ListenerURL:    {TypeURL: version.V3ListenerURL, TypeName: xdsresource.ListenerResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericListenerResourceTypeDecoder(c)},
						version.V3RouteConfigURL: {TypeURL: version.V3RouteConfigURL, TypeName: xdsresource.RouteConfigTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericRouteConfigResourceTypeDecoder()},
						version.V3ClusterURL:     {TypeURL: version.V3ClusterURL, TypeName: xdsresource.ClusterResourceTypeName, AllResourcesRequiredInSotW: true, Decoder: xdsresource.NewGenericClusterResourceTypeDecoder(c, gServerCfgMap)},
						version.V3EndpointsURL:   {TypeURL: version.V3EndpointsURL, TypeName: xdsresource.EndpointsResourceTypeName, AllResourcesRequiredInSotW: false, Decoder: xdsresource.NewGenericEndpointsResourceTypeDecoder()},
					},
					MetricsReporter: &metricsReporter{recorder: stats.NewTestMetricsRecorder(), target: testTargetName},
					TransportBuilder: grpctransport.NewBuilder(map[string]grpctransport.Config{
						"insecure": {
							Credentials: insecure.NewBundle(),
							GRPCNewClient: func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
								opts = append(opts, serverCfg.DialOptions()...)
								return grpc.NewClient(target, opts...)
							}},
					}),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bootstrapConfig, err := bootstrap.NewConfigFromContents(tt.bootstrapContents)
			if err != nil {
				t.Fatalf("Failed to create bootstrap config: %v", err)
			}
			gotCfg, err := buildXDSClientConfig(bootstrapConfig, stats.NewTestMetricsRecorder(), testTargetName, 0)
			if err != nil {
				t.Fatalf("Failed to build XDSClientConfig: %v", err)
			}

			wantCfg := tt.wantXDSClientConfig(bootstrapConfig)

			unexportedTypeOpts := cmpopts.IgnoreUnexported(clients.Node{}, grpctransport.Builder{})
			ignoreTypeOpts := cmpopts.IgnoreTypes(sync.Mutex{})
			resourceTypeCmpOpts := cmp.Comparer(func(a, b xdsclient.ResourceType) bool {
				return a.TypeURL == b.TypeURL && a.TypeName == b.TypeName && a.AllResourcesRequiredInSotW == b.AllResourcesRequiredInSotW && reflect.TypeOf(a.Decoder) == reflect.TypeOf(b.Decoder)
			})
			metricsReporterCmpOpts := cmp.Comparer(func(a, b clients.MetricsReporter) bool {
				if (a == nil) != (b == nil) {
					return false
				}
				if a == nil { // Both are nil
					return true
				}
				// Both are non-nil, compare type and target.
				aConcrete, aOK := a.(*metricsReporter)
				bConcrete, bOK := b.(*metricsReporter)
				if !(aOK && bOK && aConcrete.target == bConcrete.target) {
					return false
				}
				// Compare recorder by type.
				if (aConcrete.recorder == nil) != (bConcrete.recorder == nil) {
					return false
				}
				// If both are nil, recorder check passes. If both non-nil, check types.
				return aConcrete.recorder == nil || reflect.TypeOf(aConcrete.recorder) == reflect.TypeOf(bConcrete.recorder)
			})
			transportBuilderCmpOpts := cmp.Comparer(func(a, b grpctransport.Config) bool {
				// Compare Credentials by type
				credsEqual := true
				if (a.Credentials == nil) != (b.Credentials == nil) {
					credsEqual = false
				} else if a.Credentials != nil && reflect.TypeOf(a.Credentials) != reflect.TypeOf(b.Credentials) {
					credsEqual = false
				}
				// Compare GRPCNewClient by nil-ness
				newClientEqual := (a.GRPCNewClient == nil) == (b.GRPCNewClient == nil)
				return credsEqual && newClientEqual
			})

			if diff := cmp.Diff(wantCfg, gotCfg, protocmp.Transform(), unexportedTypeOpts, ignoreTypeOpts, resourceTypeCmpOpts, metricsReporterCmpOpts, transportBuilderCmpOpts); diff != "" {
				t.Errorf("buildXDSClientConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
