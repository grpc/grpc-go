// +build go1.12

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
	"strconv"
	"testing"
	"time"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/client/bootstrap"
	"google.golang.org/grpc/xds/internal/version"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
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
	var registerForTesting bool
	if resolver.Get(c2pScheme) == nil {
		// If env var to enable c2p is not set, the resolver isn't registered.
		// Need to register and unregister in defer.
		registerForTesting = true
		resolver.Register(&c2pResolverBuilder{})
	}
	oldDNS := resolver.Get("dns")
	resolver.Register(testDNSResolver)
	oldXDS := resolver.Get("xds")
	resolver.Register(testXDSResolver)
	return func() {
		if oldDNS != nil {
			resolver.Register(oldDNS)
		} else {
			resolver.UnregisterForTesting("dns")
		}
		if oldXDS != nil {
			resolver.Register(oldXDS)
		} else {
			resolver.UnregisterForTesting("xds")
		}
		if registerForTesting {
			resolver.UnregisterForTesting(c2pScheme)
		}
	}
}

// Test that when bootstrap env is set, fallback to DNS.
func TestBuildWithBootstrapEnvSet(t *testing.T) {
	defer replaceResolvers()()
	builder := resolver.Get(c2pScheme)

	for i, envP := range []*string{&env.BootstrapFileName, &env.BootstrapFileContent} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Set bootstrap config env var.
			oldEnv := *envP
			*envP = "does not matter"
			defer func() { *envP = oldEnv }()

			// Build should return DNS, not xDS.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			if r != testDNSResolver {
				t.Fatalf("want dns resolver, got %#v", r)
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

type testXDSClient struct {
	closed chan struct{}
}

func (c *testXDSClient) Close() {
	c.closed <- struct{}{}
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
				oldURI := env.C2PResolverTestOnlyTrafficDirectorURI
				env.C2PResolverTestOnlyTrafficDirectorURI = tt.tdURI
				defer func() {
					env.C2PResolverTestOnlyTrafficDirectorURI = oldURI
				}()
			}

			tXDSClient := &testXDSClient{closed: make(chan struct{}, 1)}

			configCh := make(chan *bootstrap.Config, 1)
			oldNewClient := newClientWithConfig
			newClientWithConfig = func(config *bootstrap.Config) (xdsClientInterface, error) {
				configCh <- config
				return tXDSClient, nil
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
				Id:                   "C2P",
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
			wantConfig := &bootstrap.Config{
				BalancerName: tdURL,
				TransportAPI: version.TransportV3,
				NodeProto:    wantNode,
			}
			if tt.tdURI != "" {
				wantConfig.BalancerName = tt.tdURI
			}
			cmpOpts := cmp.Options{
				cmpopts.IgnoreFields(bootstrap.Config{}, "Creds"),
				protocmp.Transform(),
			}
			select {
			case c := <-configCh:
				if diff := cmp.Diff(c, wantConfig, cmpOpts); diff != "" {
					t.Fatalf("%v", diff)
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
