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
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/xdsclient"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/resolver"
)

const defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

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

// replaceResolvers unregisters the real resolvers for schemes `dns` and `xds`
// and registers test resolvers instead. This allows the test to verify that
// expected resolvers are built.
func replaceResolvers(t *testing.T) {
	oldDNS := resolver.Get("dns")
	resolver.Register(testDNSResolver)
	oldXDS := resolver.Get("xds")
	resolver.Register(testXDSResolver)
	t.Cleanup(func() {
		resolver.Register(oldDNS)
		resolver.Register(oldXDS)
	})
}

func simulateRunningOnGCE(t *testing.T, gce bool) {
	oldOnGCE := onGCE
	onGCE = func() bool { return gce }
	t.Cleanup(func() { onGCE = oldOnGCE })
}

// ensure universeDomain is set to the expected default,
// and clean it up again after the test.
func useCleanUniverseDomain(t *testing.T) {
	universeDomainMu.Lock()
	defer universeDomainMu.Unlock()
	if universeDomain != "" {
		t.Fatalf("universe domain unexpectedly initialized: %v", universeDomain)
	}
	t.Cleanup(func() {
		universeDomainMu.Lock()
		universeDomain = ""
		universeDomainMu.Unlock()
	})
}

// Tests the scenario where the bootstrap env vars are set and we're running on
// GCE. The test builds a google-c2p resolver and verifies that an xDS resolver
// is built and that we don't fallback to DNS (because federation is enabled by
// default).
func (s) TestBuildWithBootstrapEnvSet(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
	useCleanUniverseDomain(t)

	builder := resolver.Get(c2pScheme)
	for i, envP := range []*string{&envconfig.XDSBootstrapFileName, &envconfig.XDSBootstrapFileContent} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Set bootstrap config env var.
			oldEnv := *envP
			*envP = "does not matter"
			defer func() { *envP = oldEnv }()

			// Override xDS client pool.
			oldXdsClientPool := xdsClientPool
			xdsClientPool = xdsclient.NewPool(nil)
			defer func() { xdsClientPool = oldXdsClientPool }()

			// Build the google-c2p resolver.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			defer r.Close()

			// Build should return xDS, not DNS.
			if r != testXDSResolver {
				t.Fatalf("Build() returned %#v, want xds resolver", r)
			}
		})
	}
}

// Tests the scenario where we are not running on GCE.  The test builds a
// google-c2p resolver and verifies that we fallback to DNS.
func (s) TestBuildNotOnGCE(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, false)
	useCleanUniverseDomain(t)
	builder := resolver.Get(c2pScheme)

	// Build the google-c2p resolver.
	r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("failed to build resolver: %v", err)
	}
	defer r.Close()

	// Build should return DNS, not xDS.
	if r != testDNSResolver {
		t.Fatalf("Build() returned %#v, want dns resolver", r)
	}
}

func bootstrapConfig(t *testing.T, opts bootstrap.ConfigOptionsForTesting) *bootstrap.Config {
	t.Helper()

	contents, err := bootstrap.NewContentsForTesting(opts)
	if err != nil {
		t.Fatalf("Failed to create bootstrap contents: %v", err)
	}
	cfg, err := bootstrap.NewConfigFromContents(contents)
	if err != nil {
		t.Fatalf("Failed to create bootstrap config: %v", err)
	}
	return cfg
}

// Test that when a google-c2p resolver is built, the xDS client is built with
// the expected config.
func (s) TestBuildXDS(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
	useCleanUniverseDomain(t)
	builder := resolver.Get(c2pScheme)

	// Override the zone returned by the metadata server.
	oldGetZone := getZone
	getZone = func(time.Duration) string { return "test-zone" }
	defer func() { getZone = oldGetZone }()

	// Override the random func used in the node ID.
	origRandInd := randInt
	randInt = func() int { return 666 }
	defer func() { randInt = origRandInd }()

	for _, tt := range []struct {
		desc                string
		ipv6Capable         bool
		tdURIOverride       string
		wantBootstrapConfig *bootstrap.Config
	}{
		{
			desc: "ipv6 false",
			wantBootstrapConfig: bootstrapConfig(t, bootstrap.ConfigOptionsForTesting{
				Servers: []byte(`[{
					"server_uri": "dns:///directpath-pa.googleapis.com",
					"channel_creds": [{"type": "google_default"}],
					"server_features": ["ignore_resource_deletion"]
  				}]`),
				Authorities: map[string]json.RawMessage{
					"traffic-director-c2p.xds.googleapis.com": []byte(`{
							"xds_servers": [
  								{
								    "server_uri": "dns:///directpath-pa.googleapis.com",
								    "channel_creds": [{"type": "google_default"}],
								    "server_features": ["ignore_resource_deletion"]
  								}
							]
						}`),
				},
				Node: []byte(`{
					  "id": "C2P-666",
					  "locality": {"zone": "test-zone"}
					}`),
			}),
		},
		{
			desc:        "ipv6 true",
			ipv6Capable: true,
			wantBootstrapConfig: bootstrapConfig(t, bootstrap.ConfigOptionsForTesting{
				Servers: []byte(`[{
					"server_uri": "dns:///directpath-pa.googleapis.com",
					"channel_creds": [{"type": "google_default"}],
					"server_features": ["ignore_resource_deletion"]
  				}]`),
				Authorities: map[string]json.RawMessage{
					"traffic-director-c2p.xds.googleapis.com": []byte(`{
							"xds_servers": [
  								{
								    "server_uri": "dns:///directpath-pa.googleapis.com",
								    "channel_creds": [{"type": "google_default"}],
								    "server_features": ["ignore_resource_deletion"]
  								}
							]
						}`),
				},
				Node: []byte(`{
					  "id": "C2P-666",
					  "locality": {"zone": "test-zone"},
			  			"metadata": {
							"TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE": true
			  			}
					}`),
			}),
		},
		{
			desc:          "override TD URI",
			ipv6Capable:   true,
			tdURIOverride: "test-uri",
			wantBootstrapConfig: bootstrapConfig(t, bootstrap.ConfigOptionsForTesting{
				Servers: []byte(`[{
					"server_uri": "test-uri",
					"channel_creds": [{"type": "google_default"}],
					"server_features": ["ignore_resource_deletion"]
  				}]`),
				Authorities: map[string]json.RawMessage{
					"traffic-director-c2p.xds.googleapis.com": []byte(`{
							"xds_servers": [
  								{
								    "server_uri": "test-uri",
								    "channel_creds": [{"type": "google_default"}],
								    "server_features": ["ignore_resource_deletion"]
  								}
							]
						}`),
				},
				Node: []byte(`{
					  "id": "C2P-666",
					  "locality": {"zone": "test-zone"},
			  			"metadata": {
							"TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE": true
			  			}
					}`),
			}),
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			// Override IPv6 capability returned by the metadata server.
			oldGetIPv6Capability := getIPv6Capable
			getIPv6Capable = func(time.Duration) bool { return tt.ipv6Capable }
			defer func() { getIPv6Capable = oldGetIPv6Capability }()

			// Override TD URI test only env var.
			if tt.tdURIOverride != "" {
				oldURI := envconfig.C2PResolverTestOnlyTrafficDirectorURI
				envconfig.C2PResolverTestOnlyTrafficDirectorURI = tt.tdURIOverride
				defer func() { envconfig.C2PResolverTestOnlyTrafficDirectorURI = oldURI }()
			}

			// Override xDS client pool.
			oldXdsClientPool := xdsClientPool
			xdsClientPool = xdsclient.NewPool(nil)
			defer func() { xdsClientPool = oldXdsClientPool }()

			getIPv6Capable = func(time.Duration) bool { return tt.ipv6Capable }
			defer func() { getIPv6Capable = oldGetIPv6Capability }()

			// Build the google-c2p resolver.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			defer r.Close()

			// Build should return xDS, not DNS.
			if r != testXDSResolver {
				t.Fatalf("Build() returned %#v, want xds resolver", r)
			}

			gotConfig := xdsClientPool.BootstrapConfigForTesting()
			if gotConfig == nil {
				t.Fatalf("Failed to get bootstrap config: %v", err)
			}
			if diff := cmp.Diff(tt.wantBootstrapConfig, gotConfig); diff != "" {
				t.Fatalf("Unexpected diff in bootstrap config (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDialFailsWhenTargetContainsAuthority attempts to Dial a target URI of
// google-c2p scheme with a non-empty authority and verifies that it fails with
// an expected error.
func (s) TestBuildFailsWhenCalledWithAuthority(t *testing.T) {
	useCleanUniverseDomain(t)
	uri := "google-c2p://an-authority/resource"
	cc, err := grpc.NewClient(uri, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to create a client for server: %v", err)
	}
	defer func() {
		if cc != nil {
			cc.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	client := testgrpc.NewTestServiceClient(cc)
	_, err = client.EmptyCall(ctx, &testpb.Empty{})
	wantErr := "google-c2p URI scheme does not support authorities"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("client.EmptyCall(%s) returned error: %v, want: %v", uri, err, wantErr)
	}
}

func (s) TestSetUniverseDomainNonDefault(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
	useCleanUniverseDomain(t)
	builder := resolver.Get(c2pScheme)

	// Override the zone returned by the metadata server.
	oldGetZone := getZone
	getZone = func(time.Duration) string { return "test-zone" }
	defer func() { getZone = oldGetZone }()

	// Override IPv6 capability returned by the metadata server.
	oldGetIPv6Capability := getIPv6Capable
	getIPv6Capable = func(time.Duration) bool { return false }
	defer func() { getIPv6Capable = oldGetIPv6Capability }()

	// Override the random func used in the node ID.
	origRandInd := randInt
	randInt = func() int { return 666 }
	defer func() { randInt = origRandInd }()

	// Set the universe domain
	testUniverseDomain := "test-universe-domain.test"
	if err := SetUniverseDomain(testUniverseDomain); err != nil {
		t.Fatalf("SetUniverseDomain(%s) failed: %v", testUniverseDomain, err)
	}

	// Now set universe domain to something different, it should fail
	domain := "test-universe-domain-2.test"
	err := SetUniverseDomain(domain)
	wantErr := "already set"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("googlec2p.SetUniverseDomain(%s) returned error: %v, want: %v", domain, err, wantErr)
	}

	// Now explicitly set universe domain to the default, it should also fail
	domain = "googleapis.com"
	err = SetUniverseDomain(domain)
	wantErr = "already set"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("googlec2p.SetUniverseDomain(%s) returned error: %v, want: %v", domain, err, wantErr)
	}

	// Now set universe domain to the original value, it should work
	if err := SetUniverseDomain(testUniverseDomain); err != nil {
		t.Fatalf("googlec2p.SetUniverseDomain(%s) failed: %v", testUniverseDomain, err)
	}

	// Override xDS client pool.
	oldXdsClientPool := xdsClientPool
	xdsClientPool = xdsclient.NewPool(nil)
	defer func() { xdsClientPool = oldXdsClientPool }()

	// Build the google-c2p resolver.
	r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("failed to build resolver: %v", err)
	}
	defer r.Close()

	// Build should return xDS, not DNS.
	if r != testXDSResolver {
		t.Fatalf("Build() returned %#v, want xds resolver", r)
	}

	gotConfig := xdsClientPool.BootstrapConfigForTesting()
	if gotConfig == nil {
		t.Fatalf("Failed to get bootstrap config: %v", err)
	}

	// Check that we use directpath-pa.test-universe-domain.test in the
	// bootstrap config.
	wantBootstrapConfig := bootstrapConfig(t, bootstrap.ConfigOptionsForTesting{
		Servers: []byte(`[{
					"server_uri": "dns:///directpath-pa.test-universe-domain.test",
					"channel_creds": [{"type": "google_default"}],
					"server_features": ["ignore_resource_deletion"]
  				}]`),
		Authorities: map[string]json.RawMessage{
			"traffic-director-c2p.xds.googleapis.com": []byte(`{
							"xds_servers": [
  								{
								    "server_uri": "dns:///directpath-pa.test-universe-domain.test",
								    "channel_creds": [{"type": "google_default"}],
								    "server_features": ["ignore_resource_deletion"]
  								}
							]
						}`),
		},
		Node: []byte(`{
					  "id": "C2P-666",
					  "locality": {"zone": "test-zone"}
					}`),
	})
	if diff := cmp.Diff(wantBootstrapConfig, gotConfig); diff != "" {
		t.Fatalf("Unexpected diff in bootstrap config (-want +got):\n%s", diff)
	}
}

func (s) TestDefaultUniverseDomain(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
	useCleanUniverseDomain(t)
	builder := resolver.Get(c2pScheme)

	// Override the zone returned by the metadata server.
	oldGetZone := getZone
	getZone = func(time.Duration) string { return "test-zone" }
	defer func() { getZone = oldGetZone }()

	// Override IPv6 capability returned by the metadata server.
	oldGetIPv6Capability := getIPv6Capable
	getIPv6Capable = func(time.Duration) bool { return false }
	defer func() { getIPv6Capable = oldGetIPv6Capability }()

	// Override the random func used in the node ID.
	origRandInd := randInt
	randInt = func() int { return 666 }
	defer func() { randInt = origRandInd }()

	// Override xDS client pool.
	oldXdsClientPool := xdsClientPool
	xdsClientPool = xdsclient.NewPool(nil)
	defer func() { xdsClientPool = oldXdsClientPool }()

	// Build the google-c2p resolver.
	r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("failed to build resolver: %v", err)
	}
	defer r.Close()

	// Build should return xDS, not DNS.
	if r != testXDSResolver {
		t.Fatalf("Build() returned %#v, want xds resolver", r)
	}

	gotConfig := xdsClientPool.BootstrapConfigForTesting()
	if gotConfig == nil {
		t.Fatalf("Failed to get bootstrap config: %v", err)
	}

	// Check that we use directpath-pa.googleapis.com in the bootstrap config
	wantBootstrapConfig := bootstrapConfig(t, bootstrap.ConfigOptionsForTesting{
		Servers: []byte(`[{
					"server_uri": "dns:///directpath-pa.googleapis.com",
					"channel_creds": [{"type": "google_default"}],
					"server_features": ["ignore_resource_deletion"]
  				}]`),
		Authorities: map[string]json.RawMessage{
			"traffic-director-c2p.xds.googleapis.com": []byte(`{
							"xds_servers": [
  								{
								    "server_uri": "dns:///directpath-pa.googleapis.com",
								    "channel_creds": [{"type": "google_default"}],
								    "server_features": ["ignore_resource_deletion"]
  								}
							]
						}`),
		},
		Node: []byte(`{
					  "id": "C2P-666",
					  "locality": {"zone": "test-zone"}
					}`),
	})
	if diff := cmp.Diff(wantBootstrapConfig, gotConfig); diff != "" {
		t.Fatalf("Unexpected diff in bootstrap config (-want +got):\n%s", diff)
	}

	// Now set universe domain to something different than the default, it should fail
	domain := "test-universe-domain.test"
	err = SetUniverseDomain(domain)
	wantErr := "already set"
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("googlec2p.SetUniverseDomain(%s) returned error: %v, want: %v", domain, err, wantErr)
	}

	// Now explicitly set universe domain to the default, it should work
	domain = "googleapis.com"
	if err := SetUniverseDomain(domain); err != nil {
		t.Fatalf("googlec2p.SetUniverseDomain(%s) failed: %v", domain, err)
	}
}

func (s) TestSetUniverseDomainEmptyString(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
	useCleanUniverseDomain(t)
	wantErr := "cannot be empty"
	err := SetUniverseDomain("")
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("googlec2p.SetUniverseDomain(\"\") returned error: %v, want: %v", err, wantErr)
	}
}
