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

package google

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials"
	icredentials "google.golang.org/grpc/internal/credentials"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/resolver"
)

var defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

type testCreds struct {
	credentials.TransportCredentials
	typ string
}

func (c *testCreds) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, &testAuthInfo{typ: c.typ}, nil
}

func (c *testCreds) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, &testAuthInfo{typ: c.typ}, nil
}

type testAuthInfo struct {
	typ string
}

func (t *testAuthInfo) AuthType() string {
	return t.typ
}

type testPerRPCCreds struct {
	md map[string]string
}

func (c *testPerRPCCreds) RequireTransportSecurity() bool {
	return true
}

func (c *testPerRPCCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return c.md, nil
}

var (
	testTLS  = &testCreds{typ: "tls"}
	testALTS = &testCreds{typ: "alts"}
)

func overrideNewCredsFuncs() func() {
	origNewTLS := newTLS
	newTLS = func() credentials.TransportCredentials {
		return testTLS
	}
	origNewALTS := newALTS
	newALTS = func() credentials.TransportCredentials {
		return testALTS
	}
	origNewADC := newADC
	newADC = func(context.Context) (credentials.PerRPCCredentials, error) {
		// We do not use perRPC creds in this test. It is safe to return nil here.
		return nil, nil
	}

	return func() {
		newTLS = origNewTLS
		newALTS = origNewALTS
		newADC = origNewADC
	}
}

// TestClientHandshakeBasedOnClusterName that by default (without switching
// modes), ClientHandshake does either tls or alts base on the cluster name in
// attributes.
func (s) TestClientHandshakeBasedOnClusterName(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	defer overrideNewCredsFuncs()()
	for bundleTyp, tc := range map[string]credentials.Bundle{
		"defaultCredsWithOptions": NewDefaultCredentialsWithOptions(DefaultCredentialsOptions{}),
		"defaultCreds":            NewDefaultCredentials(),
		"computeCreds":            NewComputeEngineCredentials(),
	} {
		tests := []struct {
			name    string
			ctx     context.Context
			wantTyp string
		}{
			{
				name:    "no cluster name",
				ctx:     ctx,
				wantTyp: "tls",
			},
			{
				name: "with non-CFE cluster name",
				ctx: icredentials.NewClientHandshakeInfoContext(ctx, credentials.ClientHandshakeInfo{
					Attributes: xds.SetXDSHandshakeClusterName(resolver.Address{}, "lalala").Attributes,
				}),
				// non-CFE backends should use alts.
				wantTyp: "alts",
			},
			{
				name: "with CFE cluster name",
				ctx: icredentials.NewClientHandshakeInfoContext(ctx, credentials.ClientHandshakeInfo{
					Attributes: xds.SetXDSHandshakeClusterName(resolver.Address{}, "google_cfe_bigtable.googleapis.com").Attributes,
				}),
				// CFE should use tls.
				wantTyp: "tls",
			},
			{
				name: "with xdstp CFE cluster name",
				ctx: icredentials.NewClientHandshakeInfoContext(ctx, credentials.ClientHandshakeInfo{
					Attributes: xds.SetXDSHandshakeClusterName(resolver.Address{}, "xdstp://traffic-director-c2p.xds.googleapis.com/envoy.config.cluster.v3.Cluster/google_cfe_bigtable.googleapis.com").Attributes,
				}),
				// CFE should use tls.
				wantTyp: "tls",
			},
			{
				name: "with xdstp non-CFE cluster name",
				ctx: icredentials.NewClientHandshakeInfoContext(ctx, credentials.ClientHandshakeInfo{
					Attributes: xds.SetXDSHandshakeClusterName(resolver.Address{}, "xdstp://other.com/envoy.config.cluster.v3.Cluster/google_cfe_bigtable.googleapis.com").Attributes,
				}),
				// non-CFE should use atls.
				wantTyp: "alts",
			},
		}
		for _, tt := range tests {
			t.Run(bundleTyp+" "+tt.name, func(t *testing.T) {
				_, info, err := tc.TransportCredentials().ClientHandshake(tt.ctx, "", nil)
				if err != nil {
					t.Fatalf("ClientHandshake failed: %v", err)
				}
				if gotType := info.AuthType(); gotType != tt.wantTyp {
					t.Fatalf("unexpected authtype: %v, want: %v", gotType, tt.wantTyp)
				}

				_, infoServer, err := tc.TransportCredentials().ServerHandshake(nil)
				if err != nil {
					t.Fatalf("ClientHandshake failed: %v", err)
				}
				// ServerHandshake should always do TLS.
				if gotType := infoServer.AuthType(); gotType != "tls" {
					t.Fatalf("unexpected server authtype: %v, want: %v", gotType, "tls")
				}
			})
		}
	}
}

func TestDefaultCredentialsWithOptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	md1 := map[string]string{"foo": "tls"}
	md2 := map[string]string{"foo": "alts"}
	tests := []struct {
		desc             string
		defaultCredsOpts DefaultCredentialsOptions
		authInfo         credentials.AuthInfo
		wantedMetadata   map[string]string
	}{
		{
			desc: "no ALTSPerRPCCreds with tls channel",
			defaultCredsOpts: DefaultCredentialsOptions{
				PerRPCCreds: &testPerRPCCreds{
					md: md1,
				},
			},
			authInfo:       &testAuthInfo{typ: "tls"},
			wantedMetadata: md1,
		},
		{
			desc: "no ALTSPerRPCCreds with alts channel",
			defaultCredsOpts: DefaultCredentialsOptions{
				PerRPCCreds: &testPerRPCCreds{
					md: md1,
				},
			},
			authInfo:       &testAuthInfo{typ: "alts"},
			wantedMetadata: md1,
		},
		{
			desc: "ALTSPerRPCCreds specified with tls channel",
			defaultCredsOpts: DefaultCredentialsOptions{
				PerRPCCreds: &testPerRPCCreds{
					md: md1,
				},
				ALTSPerRPCCreds: &testPerRPCCreds{
					md: md2,
				},
			},
			authInfo:       &testAuthInfo{typ: "tls"},
			wantedMetadata: md1,
		},
		{
			desc: "ALTSPerRPCCreds specified with alts channel",
			defaultCredsOpts: DefaultCredentialsOptions{
				PerRPCCreds: &testPerRPCCreds{
					md: md1,
				},
				ALTSPerRPCCreds: &testPerRPCCreds{
					md: md2,
				},
			},
			authInfo:       &testAuthInfo{typ: "alts"},
			wantedMetadata: md2,
		},
		{
			desc: "ALTSPerRPCCreds specified with unknown channel",
			defaultCredsOpts: DefaultCredentialsOptions{
				PerRPCCreds: &testPerRPCCreds{
					md: md1,
				},
				ALTSPerRPCCreds: &testPerRPCCreds{
					md: md2,
				},
			},
			authInfo:       &testAuthInfo{typ: "foo"},
			wantedMetadata: md1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			bundle := NewDefaultCredentialsWithOptions(tc.defaultCredsOpts)
			ri := credentials.RequestInfo{AuthInfo: tc.authInfo}
			ctx := credentials.NewContextWithRequestInfo(ctx, ri)
			got, err := bundle.PerRPCCredentials().GetRequestMetadata(ctx, "uri")
			if err != nil {
				t.Fatalf("Bundle's PerRPCCredentials().GetRequestMetadata() unexpected error = %v", err)
			}
			if diff := cmp.Diff(got, tc.wantedMetadata); diff != "" {
				t.Errorf("Unexpected request metadata from bundle's PerRPCCredentials. Diff (-got +want):\n%v", diff)
			}
		})
	}
}
