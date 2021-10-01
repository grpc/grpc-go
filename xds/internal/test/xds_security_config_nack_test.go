//go:build !386
// +build !386

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

package xds_test

import (
	"context"
	"fmt"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	xdscreds "google.golang.org/grpc/credentials/xds"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func (s) TestUnmarshalListener_WithUpdateValidatorFunc(t *testing.T) {
	const (
		serviceName                     = "my-service-client-side-xds"
		missingIdentityProviderInstance = "missing-identity-provider-instance"
		missingRootProviderInstance     = "missing-root-provider-instance"
	)
	managementServer, nodeID, bootstrapContents, resolver, cleanup1 := setupManagementServer(t)
	defer cleanup1()

	lis, cleanup2 := setupGRPCServer(t, bootstrapContents)
	defer cleanup2()

	// Grab the host and port of the server and create client side xDS
	// resources corresponding to it.
	host, port, err := hostPortFromListener(lis)
	if err != nil {
		t.Fatalf("failed to retrieve host and port of server: %v", err)
	}

	// Create xDS resources to be consumed on the client side. This
	// includes the listener, route configuration, cluster (with
	// security configuration) and endpoint resources.
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       host,
		Port:       port,
		SecLevel:   e2e.SecurityLevelMTLS,
	})

	tests := []struct {
		name           string
		securityConfig *v3corepb.TransportSocket
		wantErr        bool
	}{
		{
			name: "both identity and root providers are not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: missingIdentityProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: missingRootProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "only identity provider is not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: missingIdentityProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: e2e.ServerSideCertProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "only root provider is not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: e2e.ServerSideCertProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: missingRootProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "both identity and root providers are present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.DownstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: e2e.ServerSideCertProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: e2e.ServerSideCertProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: false,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Create an inbound xDS listener resource for the server side.
			inboundLis := e2e.DefaultServerListener(host, port, e2e.SecurityLevelMTLS)
			for _, fc := range inboundLis.GetFilterChains() {
				fc.TransportSocket = test.securityConfig
			}

			// Setup the management server with client and server resources.
			if len(resources.Listeners) == 1 {
				resources.Listeners = append(resources.Listeners, inboundLis)
			} else {
				resources.Listeners[1] = inboundLis
			}
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			// Create client-side xDS credentials with an insecure fallback.
			creds, err := xdscreds.NewClientCredentials(xdscreds.ClientOptions{FallbackCreds: insecure.NewCredentials()})
			if err != nil {
				t.Fatal(err)
			}

			// Create a ClientConn with the xds scheme and make an RPC.
			cc, err := grpc.DialContext(ctx, fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(creds), grpc.WithResolvers(resolver))
			if err != nil {
				t.Fatalf("failed to dial local test server: %v", err)
			}
			defer cc.Close()

			// Make a context with a shorter timeout from the top level test
			// context for cases where we expect failures.
			timeout := defaultTestTimeout
			if test.wantErr {
				timeout = defaultTestShortTimeout
			}
			ctx2, cancel2 := context.WithTimeout(ctx, timeout)
			defer cancel2()
			client := testpb.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx2, &testpb.Empty{}, grpc.WaitForReady(true)); (err != nil) != test.wantErr {
				t.Fatalf("EmptyCall() returned err: %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func (s) TestUnmarshalCluster_WithUpdateValidatorFunc(t *testing.T) {
	const (
		serviceName                     = "my-service-client-side-xds"
		missingIdentityProviderInstance = "missing-identity-provider-instance"
		missingRootProviderInstance     = "missing-root-provider-instance"
	)

	tests := []struct {
		name           string
		securityConfig *v3corepb.TransportSocket
		wantErr        bool
	}{
		{
			name: "both identity and root providers are not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: missingIdentityProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: missingRootProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "only identity provider is not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: missingIdentityProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: e2e.ClientSideCertProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "only root provider is not present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: e2e.ClientSideCertProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: missingRootProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "both identity and root providers are present in bootstrap",
			securityConfig: &v3corepb.TransportSocket{
				Name: "envoy.transport_sockets.tls",
				ConfigType: &v3corepb.TransportSocket_TypedConfig{
					TypedConfig: testutils.MarshalAny(&v3tlspb.UpstreamTlsContext{
						CommonTlsContext: &v3tlspb.CommonTlsContext{
							TlsCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
								InstanceName: e2e.ClientSideCertProviderInstance,
							},
							ValidationContextType: &v3tlspb.CommonTlsContext_ValidationContext{
								ValidationContext: &v3tlspb.CertificateValidationContext{
									CaCertificateProviderInstance: &v3tlspb.CertificateProviderPluginInstance{
										InstanceName: e2e.ClientSideCertProviderInstance,
									},
								},
							},
						},
					}),
				},
			},
			wantErr: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// setupManagementServer() sets up a bootstrap file with certificate
			// provider instance names: `e2e.ServerSideCertProviderInstance` and
			// `e2e.ClientSideCertProviderInstance`.
			managementServer, nodeID, _, resolver, cleanup1 := setupManagementServer(t)
			defer cleanup1()

			port, cleanup2 := clientSetup(t, &testService{})
			defer cleanup2()

			// This creates a `Cluster` resource with a security config which
			// refers to `e2e.ClientSideCertProviderInstance` for both root and
			// identity certs.
			resources := e2e.DefaultClientResources(e2e.ResourceParams{
				DialTarget: serviceName,
				NodeID:     nodeID,
				Host:       "localhost",
				Port:       port,
				SecLevel:   e2e.SecurityLevelMTLS,
			})
			resources.Clusters[0].TransportSocket = test.securityConfig
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
			if err != nil {
				t.Fatalf("failed to dial local test server: %v", err)
			}
			defer cc.Close()

			// Make a context with a shorter timeout from the top level test
			// context for cases where we expect failures.
			timeout := defaultTestTimeout
			if test.wantErr {
				timeout = defaultTestShortTimeout
			}
			ctx2, cancel2 := context.WithTimeout(ctx, timeout)
			defer cancel2()
			client := testpb.NewTestServiceClient(cc)
			if _, err := client.EmptyCall(ctx2, &testpb.Empty{}, grpc.WaitForReady(true)); (err != nil) != test.wantErr {
				t.Fatalf("EmptyCall() returned err: %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
