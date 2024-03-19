/*
 *
 * Copyright 2021 gRPC authors.
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

package googledirectpath

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

type emptyResolver struct {
	resolver.Resolver
	scheme string
}

func (er *emptyResolver) Build(_ resolver.Target, _ resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	return er, nil
}

func (er *emptyResolver) Scheme() string {
	return er.scheme
}

func (er *emptyResolver) Close() {}

var (
	testDNSResolver = &emptyResolver{scheme: "dns"}
	testXDSResolver = &emptyResolver{scheme: "xds"}
)

func replaceResolvers() func() {
	oldDNS := resolver.Get("dns")
	resolver.Register(testDNSResolver)
	oldXDS := resolver.Get("xds")
	resolver.Register(testXDSResolver)
	return func() {
		resolver.Register(oldDNS)
		resolver.Register(oldXDS)
	}
}

type testXDSClient struct {
	xdsclient.XDSClient
	closed chan struct{}
}

func (c *testXDSClient) Close() {
	c.closed <- struct{}{}
}

// Test that when bootstrap env is set and we're running on GCE, don't fallback to DNS (because
// federation is enabled by default).
func TestBuildWithBootstrapEnvSet(t *testing.T) {
	defer replaceResolvers()()
	builder := resolver.Get(c2pScheme)

	// make the test behave the ~same whether it's running on or off GCE
	oldOnGCE := onGCE
	onGCE = func() bool { return true }
	defer func() { onGCE = oldOnGCE }()

	// don't actually read the bootstrap file contents
	xdsClient := &testXDSClient{closed: make(chan struct{}, 1)}
	oldNewClient := newClientWithConfig
	newClientWithConfig = func(config *bootstrap.Config) (xdsclient.XDSClient, func(), error) {
		return xdsClient, func() { xdsClient.Close() }, nil
	}
	defer func() { newClientWithConfig = oldNewClient }()

	for i, envP := range []*string{&envconfig.XDSBootstrapFileName, &envconfig.XDSBootstrapFileContent} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Set bootstrap config env var.
			oldEnv := *envP
			*envP = "does not matter"
			defer func() { *envP = oldEnv }()

			// Build should return xDS, not DNS.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			rr := r.(*c2pResolver)
			if rrr := rr.Resolver; rrr != testXDSResolver {
				t.Fatalf("want xds resolver, got %#v", rrr)
			}
		})
	}
}

// Test that when not on GCE, fallback to DNS.
func TestBuildNotOnGCE(t *testing.T) {
	defer replaceResolvers()()
	builder := resolver.Get(c2pScheme)

	oldOnGCE := onGCE
	onGCE = func() bool { return false }
	defer func() { onGCE = oldOnGCE }()

	// Build should return DNS, not xDS.
	r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("failed to build resolver: %v", err)
	}
	if r != testDNSResolver {
		t.Fatalf("want dns resolver, got %#v", r)
	}
}

// Test that when xDS is built, the client is built with the correct config.
func TestBuildXDS(t *testing.T) {
	defer replaceResolvers()()
	builder := resolver.Get(c2pScheme)

	oldOnGCE := onGCE
	onGCE = func() bool { return true }
	defer func() { onGCE = oldOnGCE }()

	const testZone = "test-zone"
	oldGetZone := getZone
	getZone = func(time.Duration) string { return testZone }
	defer func() { getZone = oldGetZone }()

	for _, tt := range []struct {
		name  string
		ipv6  bool
		tdURI string // traffic director URI will be overridden if this is set.
	}{
		{name: "ipv6 true", ipv6: true},
		{name: "ipv6 false", ipv6: false},
		{name: "override TD URI", ipv6: true, tdURI: "test-uri"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			oldGetIPv6Capability := getIPv6Capable
			getIPv6Capable = func(time.Duration) bool { return tt.ipv6 }
			defer func() { getIPv6Capable = oldGetIPv6Capability }()

			if tt.tdURI != "" {
				oldURI := envconfig.C2PResolverTestOnlyTrafficDirectorURI
				envconfig.C2PResolverTestOnlyTrafficDirectorURI = tt.tdURI
				defer func() {
					envconfig.C2PResolverTestOnlyTrafficDirectorURI = oldURI
				}()
			}

			tXDSClient := &testXDSClient{closed: make(chan struct{}, 1)}

			configCh := make(chan *bootstrap.Config, 1)
			oldNewClient := newClientWithConfig
			newClientWithConfig = func(config *bootstrap.Config) (xdsclient.XDSClient, func(), error) {
				configCh <- config
				return tXDSClient, func() { tXDSClient.Close() }, nil
			}
			defer func() { newClientWithConfig = oldNewClient }()

			// Build should return DNS, not xDS.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			rr := r.(*c2pResolver)
			if rrr := rr.Resolver; rrr != testXDSResolver {
				t.Fatalf("want xds resolver, got %#v, ", rrr)
			}

			wantNode := &v3corepb.Node{
				Id:                   id,
				Metadata:             nil,
				Locality:             &v3corepb.Locality{Zone: testZone},
				UserAgentName:        gRPCUserAgentName,
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
				ClientFeatures:       []string{clientFeatureNoOverprovisioning},
			}
			if tt.ipv6 {
				wantNode.Metadata = &structpb.Struct{
					Fields: map[string]*structpb.Value{
						ipv6CapableMetadataName: {
							Kind: &structpb.Value_BoolValue{BoolValue: true},
						},
					},
				}
			}
			wantServerConfig, err := bootstrap.ServerConfigFromJSON([]byte(fmt.Sprintf(`{
				"server_uri": "%s",
				"channel_creds": [{"type": "google_default"}],
				"server_features": ["xds_v3", "ignore_resource_deletion", "xds.config.resource-in-sotw"]
			}`, tdURL)))
			if err != nil {
				t.Fatalf("Failed to build server bootstrap config: %v", err)
			}
			wantConfig := &bootstrap.Config{
				XDSServer: wantServerConfig,
				ClientDefaultListenerResourceNameTemplate: "%s",
				Authorities: map[string]*bootstrap.Authority{
					"traffic-director-c2p.xds.googleapis.com": {
						XDSServer:                          wantServerConfig,
						ClientListenerResourceNameTemplate: "xdstp://traffic-director-c2p.xds.googleapis.com/envoy.config.listener.v3.Listener/%s",
					},
				},
				NodeProto: wantNode,
			}
			if tt.tdURI != "" {
				wantConfig.XDSServer.ServerURI = tt.tdURI
			}
			cmpOpts := cmp.Options{
				cmpopts.IgnoreFields(bootstrap.ServerConfig{}, "Creds"),
				cmp.AllowUnexported(bootstrap.ServerConfig{}),
				protocmp.Transform(),
			}
			select {
			case gotConfig := <-configCh:
				if diff := cmp.Diff(wantConfig, gotConfig, cmpOpts); diff != "" {
					t.Fatalf("Unexpected diff in bootstrap config (-want +got):\n%s", diff)
				}
			case <-time.After(time.Second):
				t.Fatalf("timeout waiting for client config")
			}

			r.Close()
			select {
			case <-tXDSClient.closed:
			case <-time.After(time.Second):
				t.Fatalf("timeout waiting for client close")
			}
		})
	}
}

// TestDialFailsWhenTargetContainsAuthority attempts to Dial a target URI of
// google-c2p scheme with a non-empty authority and verifies that it fails with
// an expected error.
func TestBuildFailsWhenCalledWithAuthority(t *testing.T) {
	uri := "google-c2p://an-authority/resource"
	cc, err := grpc.Dial(uri, grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer func() {
		if cc != nil {
			cc.Close()
		}
	}()
	wantErr := "google-c2p URI scheme does not support authorities"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("grpc.Dial(%s) returned error: %v, want: %v", uri, err, wantErr)
	}
}
