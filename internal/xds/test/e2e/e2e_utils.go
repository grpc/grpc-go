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
 */

package e2e

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/xdsclient/xdsresource"
	"google.golang.org/grpc/resolver"
)

func verifySubConnStates(t *testing.T, scs []*channelzpb.Subchannel, want map[channelzpb.ChannelConnectivityState_State]int) {
	t.Helper()
	var scStatsCount = map[channelzpb.ChannelConnectivityState_State]int{}
	for _, sc := range scs {
		scStatsCount[sc.Data.State.State]++
	}
	if diff := cmp.Diff(scStatsCount, want); diff != "" {
		t.Fatalf("got unexpected number of subchannels in state Ready, %v, scs: %v", diff, scs)
	}
}

// NewStringP returns a pointer to the provided string.
func NewStringP(s string) *string {
	return &s
}

// This function determines the stable, canonical order for any two
// resolver.Endpoint structs.
func lessEndpoint(a, b resolver.Endpoint) bool {
	return getHash(a) < getHash(b)
}

func getHash(e resolver.Endpoint) uint64 {
	h := xxhash.New()

	// We iterate through all addresses to ensure the hash represents
	// the full endpoint identity.
	for _, addr := range e.Addresses {
		h.Write([]byte(addr.Addr))
		h.Write([]byte(addr.ServerName))
	}

	return h.Sum64()
}

// VerifyXDSConfig waits for an update on the xdsCh channel or an error on the
// errCh channel, and verifies that the received XDSConfig matches the expected
// XDS configuration.
func VerifyXDSConfig(ctx context.Context, xdsCh chan *xdsresource.XDSConfig, errCh chan error, want *xdsresource.XDSConfig) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for update from dependency manager")
	case update := <-xdsCh:
		cmpOpts := []cmp.Option{
			cmpopts.EquateEmpty(),
			cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
			cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
			cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
			cmpopts.IgnoreFields(xdsresource.ClusterUpdate{}, "Raw", "LBPolicy", "TelemetryLabels"),
			cmpopts.IgnoreFields(xdsresource.EndpointsUpdate{}, "Raw"),
			// Used for EndpointConfig.ResolutionNote and ClusterResult.Err fields.
			cmp.Transformer("ErrorsToString", func(in error) string {
				if in == nil {
					return "" // Treat nil as an empty string
				}
				s := in.Error()

				// Replace all sequences of whitespace (including newlines and
				// tabs) with a single standard space.
				s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

				// Trim any leading/trailing space that might be left over and
				// return error as string.
				return strings.TrimSpace(s)
			}),
			cmpopts.SortSlices(lessEndpoint),
		}
		if diff := cmp.Diff(update, want, cmpOpts...); diff != "" {
			return fmt.Errorf("received unexpected update from dependency manager. Diff (-got +want):\n%v", diff)
		}
	case err := <-errCh:
		return fmt.Errorf("received unexpected error from dependency manager: %v", err)
	}
	return nil
}

// CompareXDSConfig compares two XDSConfig structs and returns an error if they
// do not match.
func CompareXDSConfig(gotXDSConfig, wantXDSConfig *xdsresource.XDSConfig) error {
	cmpOpts := []cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(xdsresource.HTTPFilter{}, "Filter", "Config"),
		cmpopts.IgnoreFields(xdsresource.ListenerUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.RouteConfigUpdate{}, "Raw"),
		cmpopts.IgnoreFields(xdsresource.ClusterUpdate{}, "Raw", "LBPolicy", "TelemetryLabels"),
		cmpopts.IgnoreFields(xdsresource.EndpointsUpdate{}, "Raw"),
		// Used for EndpointConfig.ResolutionNote and ClusterResult.Err fields.
		cmp.Transformer("ErrorsToString", func(in error) string {
			if in == nil {
				return "" // Treat nil as an empty string
			}
			s := in.Error()

			// Replace all sequences of whitespace (including newlines and
			// tabs) with a single standard space.
			s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")

			// Trim any leading/trailing space that might be left over and
			// return error as string.
			return strings.TrimSpace(s)
		}),
		cmpopts.SortSlices(lessEndpoint),
	}
	if diff := cmp.Diff(gotXDSConfig, wantXDSConfig, cmpOpts...); diff != "" {
		return fmt.Errorf("received unexpected update from dependency manager. Diff (-got +want):\n%v", diff)
	}
	return nil
}

// MakeXDSConfig creates a mock XDSConfig with a standard structure containing
// a Listener, RouteConfig, VirtualHost, and a single EDS Cluster, populated
// with the provided parameters.
func MakeXDSConfig(servicename, routeConfigName, clusterName, edsServiceName, addr string) *xdsresource.XDSConfig {
	return &xdsresource.XDSConfig{
		Listener: &xdsresource.ListenerUpdate{
			APIListener: &xdsresource.HTTPConnectionManagerConfig{
				RouteConfigName: routeConfigName,
				HTTPFilters:     []xdsresource.HTTPFilter{{Name: "router"}},
			},
		},
		RouteConfig: &xdsresource.RouteConfigUpdate{
			VirtualHosts: []*xdsresource.VirtualHost{
				{
					Domains: []string{servicename},
					Routes: []*xdsresource.Route{{
						Prefix:           NewStringP("/"),
						WeightedClusters: []xdsresource.WeightedCluster{{Name: clusterName, Weight: 100}},
						ActionType:       xdsresource.RouteActionRoute,
					}},
				},
			},
		},
		VirtualHost: &xdsresource.VirtualHost{
			Domains: []string{servicename},
			Routes: []*xdsresource.Route{{
				Prefix:           NewStringP("/"),
				WeightedClusters: []xdsresource.WeightedCluster{{Name: clusterName, Weight: 100}},
				ActionType:       xdsresource.RouteActionRoute},
			},
		},
		Clusters: map[string]*xdsresource.ClusterResult{
			clusterName: {
				Config: xdsresource.ClusterConfig{Cluster: &xdsresource.ClusterUpdate{
					ClusterType:    xdsresource.ClusterTypeEDS,
					ClusterName:    clusterName,
					EDSServiceName: edsServiceName,
				},
					EndpointConfig: &xdsresource.EndpointConfig{
						EDSUpdate: &xdsresource.EndpointsUpdate{
							Localities: []xdsresource.Locality{
								{ID: clients.Locality{
									Region:  "region-1",
									Zone:    "zone-1",
									SubZone: "subzone-1",
								},
									Endpoints: []xdsresource.Endpoint{
										{
											ResolverEndpoint: resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}},
											HealthStatus:     xdsresource.EndpointHealthStatusUnknown,
											Weight:           1,
										},
									},
									Weight: 1,
								},
							},
						},
					},
				},
			},
		},
	}
}
