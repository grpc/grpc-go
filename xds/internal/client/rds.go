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
	"strings"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/grpclog"

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

func (v2c *v2Client) newRDSRequest(routeName string) *xdspb.DiscoveryRequest {
	return &xdspb.DiscoveryRequest{
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

func (v2c *v2Client) handleRDSResponse(resp *xdspb.DiscoveryResponse) error {
	cluster := ""
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		rc, ok := resource.Message.(*xdspb.RouteConfiguration)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in RDS response", resource.Message)
		}
		if !v2c.isRouteConfigurationInteresting(rc) {
			continue
		}
		cluster = getClusterFromRouteConfiguration(rc, v2c.ldsWatch.target)
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

// Caller should hold v2c.mu
func (v2c *v2Client) isRouteConfigurationInteresting(rc *xdspb.RouteConfiguration) bool {
	return v2c.rdsWatch != nil && v2c.rdsWatch.routeName == rc.GetName()
}

func getClusterFromRouteConfiguration(rc *xdspb.RouteConfiguration, target string) string {
	host := stripPort(target)
	for _, vh := range rc.GetVirtualHosts() {
		for _, domain := range vh.GetDomains() {
			// TODO: Add support for wildcard matching here.
			if domain == host {
				if len(vh.GetRoutes()) > 0 {
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

func stripPort(host string) string {
	colon := strings.LastIndexByte(host, ':')
	if colon == -1 {
		return host
	}
	return host[:colon]
}
