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
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/internal/xdsclient"
)

const defaultTestTimeout = 10 * time.Second

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

type testXDSClient struct {
	xdsclient.XDSClient
	closed chan struct{}
}

func (c *testXDSClient) Close() {
	c.closed <- struct{}{}
}

// Overrides the creation of a real xDS client with a test one.
func overrideWithTestXDSClient(t *testing.T) (*testXDSClient, chan *bootstrap.Config) {
	xdsC := &testXDSClient{closed: make(chan struct{}, 1)}
	configCh := make(chan *bootstrap.Config, 1)
	oldNewClient := newClientWithConfig
	newClientWithConfig = func(config *bootstrap.Config) (xdsclient.XDSClient, func(), error) {
		configCh <- config
		return xdsC, func() { xdsC.Close() }, nil
	}
	t.Cleanup(func() { newClientWithConfig = oldNewClient })
	return xdsC, configCh
}

// Tests the scenario where the bootstrap env vars are set and we're running on
// GCE. The test builds a google-c2p resolver and verifies that an xDS resolver
// is built and that we don't fallback to DNS (because federation is enabled by
// default).
func (s) TestBuildWithBootstrapEnvSet(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)

	builder := resolver.Get(c2pScheme)
	for i, envP := range []*string{&envconfig.XDSBootstrapFileName, &envconfig.XDSBootstrapFileContent} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			// Set bootstrap config env var.
			oldEnv := *envP
			*envP = "does not matter"
			defer func() { *envP = oldEnv }()

			overrideWithTestXDSClient(t)

			// Build the google-c2p resolver.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}
			defer r.Close()

			// Build should return xDS, not DNS.
			rr := r.(*c2pResolver)
			if rrr := rr.Resolver; rrr != testXDSResolver {
				t.Fatalf("want xds resolver, got %#v", rrr)
			}
		})
	}
}

// Tests the scenario where we are not running on GCE.  The test builds a
// google-c2p resolver and verifies that we fallback to DNS.
func (s) TestBuildNotOnGCE(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, false)
	builder := resolver.Get(c2pScheme)

	// Build the google-c2p resolver.
	r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
	if err != nil {
		t.Fatalf("failed to build resolver: %v", err)
	}
	defer r.Close()

	// Build should return DNS, not xDS.
	if r != testDNSResolver {
		t.Fatalf("want dns resolver, got %#v", r)
	}
}

// Test that when a google-c2p resolver is built, the xDS client is built with
// the expected config.
func (s) TestBuildXDS(t *testing.T) {
	replaceResolvers(t)
	simulateRunningOnGCE(t, true)
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
			wantBootstrapConfig: func() *bootstrap.Config {
				cfg, err := bootstrap.NewConfigFromContents([]byte(`{
"xds_servers": [
  {
    "server_uri": "dns:///directpath-pa.googleapis.com",
    "channel_creds": [{"type": "google_default"}],
    "server_features": ["ignore_resource_deletion"]
  }
],
"client_default_listener_resource_name_template": "%s",
"authorities": {
  "traffic-director-c2p.xds.googleapis.com": {
    "xds_servers": [
      {
        "server_uri": "dns:///directpath-pa.googleapis.com",
        "channel_creds": [{"type": "google_default"}],
        "server_features": ["ignore_resource_deletion"]
      }
	]
  }
},
"node": {
  "id": "C2P-666",
  "locality": {
  "zone": "test-zone"
  }
}
}`))
				if err != nil {
					t.Fatalf("Bootstrap parsing failure: %v", err)
				}
				return cfg
			}(),
		},
		{
			desc:        "ipv6 true",
			ipv6Capable: true,
			wantBootstrapConfig: func() *bootstrap.Config {
				cfg, err := bootstrap.NewConfigFromContents([]byte(`{
"xds_servers": [
  {
    "server_uri": "dns:///directpath-pa.googleapis.com",
    "channel_creds": [{"type": "google_default"}],
    "server_features": ["ignore_resource_deletion"]
  }
],
"client_default_listener_resource_name_template": "%s",
"authorities": {
  "traffic-director-c2p.xds.googleapis.com": {
    "xds_servers": [
      {
        "server_uri": "dns:///directpath-pa.googleapis.com",
        "channel_creds": [{"type": "google_default"}],
        "server_features": ["ignore_resource_deletion"]
      }
	]
  }
},
"node": {
  "id": "C2P-666",
  "locality": {
  "zone": "test-zone"
  },
  "metadata": {
	"TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE": true
  }
}
}`))
				if err != nil {
					t.Fatalf("Bootstrap parsing failure: %v", err)
				}
				return cfg
			}(),
		},
		{
			desc:          "override TD URI",
			ipv6Capable:   true,
			tdURIOverride: "test-uri",
			wantBootstrapConfig: func() *bootstrap.Config {
				cfg, err := bootstrap.NewConfigFromContents([]byte(`{
"xds_servers": [
  {
    "server_uri": "test-uri",
    "channel_creds": [{"type": "google_default"}],
    "server_features": ["ignore_resource_deletion"]
  }
],
"client_default_listener_resource_name_template": "%s",
"authorities": {
  "traffic-director-c2p.xds.googleapis.com": {
    "xds_servers": [
      {
        "server_uri": "test-uri",
        "channel_creds": [{"type": "google_default"}],
        "server_features": ["ignore_resource_deletion"]
      }
	]
  }
},
"node": {
  "id": "C2P-666",
  "locality": {
  "zone": "test-zone"
  },
  "metadata": {
	"TRAFFICDIRECTOR_DIRECTPATH_C2P_IPV6_CAPABLE": true
  }
}
}`))
				if err != nil {
					t.Fatalf("Bootstrap parsing failure: %v", err)
				}
				return cfg
			}(),
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

			tXDSClient, configCh := overrideWithTestXDSClient(t)

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			// Build the google-c2p resolver.
			r, err := builder.Build(resolver.Target{}, nil, resolver.BuildOptions{})
			if err != nil {
				t.Fatalf("failed to build resolver: %v", err)
			}

			// Build should return xDS, not DNS.
			rr := r.(*c2pResolver)
			if rrr := rr.Resolver; rrr != testXDSResolver {
				t.Fatalf("want xds resolver, got %#v, ", rrr)
			}

			var gotConfig *bootstrap.Config
			select {
			case gotConfig = <-configCh:
				if diff := cmp.Diff(tt.wantBootstrapConfig, gotConfig); diff != "" {
					t.Fatalf("Unexpected diff in bootstrap config (-want +got):\n%s", diff)
				}
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for new xDS client to be built")
			}

			r.Close()
			select {
			case <-tXDSClient.closed:
			case <-ctx.Done():
				t.Fatalf("Timeout waiting for xDS client to be closed")
			}
		})
	}
}

// TestDialFailsWhenTargetContainsAuthority attempts to Dial a target URI of
// google-c2p scheme with a non-empty authority and verifies that it fails with
// an expected error.
func (s) TestBuildFailsWhenCalledWithAuthority(t *testing.T) {
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
