/*
 *
 * Copyright 2019 gRPC authors.
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

package client

import (
	"fmt"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/grpclog"

	discoverypb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	rdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

func (v2c *v2Client) newRDSRequest(routeName string) *discoverypb.DiscoveryRequest {
	return &discoverypb.DiscoveryRequest{
		Node:          v2c.nodeProto,
		TypeUrl:       routeURL,
		ResourceNames: []string{routeName},
	}
}

func (v2c *v2Client) sendRDS(stream adsStream, routeName string) bool {
	if err := stream.Send(v2c.newRDSRequest(routeName)); err != nil {
		grpclog.Infof("xds: RDS request for resource %v failed: %v", routeName, err)
		return false
	}
	return true
}

func (v2c *v2Client) handleRDSResponse(resp *discoverypb.DiscoveryResponse) error {
	cluster := ""
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v.", err)
		}
		rc, ok := resource.Message.(*rdspb.RouteConfiguration)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in RDS response", resource.Message)
		}
		if !v2c.isRouteConfigurationInteresting(rc) {
			continue
		}
		cluster = v2c.getClusterFromRouteConfiguration(rc, v2c.ldsWatch.target)
		if cluster != "" {
			break
		}
	}

	var err error
	if cluster == "" {
		err = fmt.Errorf("xds: RDS response {%+v} does not contain cluster name", resp)
	}
	if v2c.rdsWatch != nil {
		v2c.rdsWatch.callback(rdsUpdate{cluster: cluster}, err)
	}
	return nil
}

func (v2c *v2Client) isRouteConfigurationInteresting(rc *rdspb.RouteConfiguration) bool {
	interesting := false
	v2c.mu.Lock()
	if v2c.rdsWatch != nil && v2c.rdsWatch.routeName == rc.GetName() {
		interesting = true
	}
	v2c.mu.Unlock()
	return interesting
}

func (v2c *v2Client) getClusterFromRouteConfiguration(rc *rdspb.RouteConfiguration, target string) string {
	for _, vh := range rc.GetVirtualHosts() {
		for _, domain := range vh.GetDomains() {
			// TODO: strip port from target before comparison
			if target == domain {
				if vh.GetRoutes() != nil {
					// The last route is the default route.
					dr := vh.Routes[len(vh.Routes)-1]
					if dr.GetMatch() == nil && dr.GetRoute() != nil {
						return dr.GetRoute().GetCluster()
					}
				}
			}
		}
	}
	return ""
}
