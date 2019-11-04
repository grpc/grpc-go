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
	ldspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
)

func (v2c *v2Client) newLDSRequest(target string) *discoverypb.DiscoveryRequest {
	return &discoverypb.DiscoveryRequest{
		Node:          v2c.nodeProto,
		TypeUrl:       listenerURL,
		ResourceNames: []string{target},
	}
}

func (v2c *v2Client) sendLDS(stream adsStream, target string) bool {
	if err := stream.Send(v2c.newLDSRequest(target)); err != nil {
		grpclog.Infof("xds: LDS request for resource %v failed: %v", target, err)
		return false
	}
	return true
}

func (v2c *v2Client) handleLDSResponse(resp *discoverypb.DiscoveryResponse) error {
	routeName := ""
	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in LDS response: %v", err)
		}
		lis, ok := resource.Message.(*ldspb.Listener)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in LDS response", resource.Message)
		}
		if !v2c.isListenerProtoInteresting(lis) {
			continue
		}
		if lis.GetApiListener() == nil {
			return fmt.Errorf("xds: no api_listener field in LDS response %+v", lis)
		}
		var apiAny ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(lis.GetApiListener().GetApiListener(), &apiAny); err != nil {
			return fmt.Errorf("xds: failed to unmarshal api_listner in LDS response: %v", err)
		}
		apiLis, ok := apiAny.Message.(*httppb.HttpConnectionManager)
		if !ok {
			return fmt.Errorf("xds: unexpected api_listener type: %T in LDS response", apiAny.Message)
		}
		switch apiLis.RouteSpecifier.(type) {
		case *httppb.HttpConnectionManager_Rds:
			routeName = apiLis.GetRds().GetRouteConfigName()
			break
		case *httppb.HttpConnectionManager_RouteConfig:
			// TODO: Add support for specifying the RouteConfiguration inline
			// in the LDS response.
			return fmt.Errorf("xds: LDS response contains RDS config inline. Not supported for now: %+v", apiLis)
		case nil:
			return fmt.Errorf("xds: no RouteSpecifier in received LDS response: %+v", apiLis)
		default:
			return fmt.Errorf("xds: unsupported type %T for RouteSpecifier in received LDS response", apiLis.RouteSpecifier)
		}
	}

	var err error
	if routeName == "" {
		err = fmt.Errorf("xds: LDS response %+v does not contain route config name", resp)
	}

	v2c.mu.Lock()
	if v2c.ldsWatch != nil {
		v2c.ldsWatch.callback(ldsUpdate{routeName: routeName}, err)
	}
	v2c.mu.Unlock()
	return nil
}

func (v2c *v2Client) isListenerProtoInteresting(lis *ldspb.Listener) bool {
	interesting := false
	v2c.mu.Lock()
	if v2c.ldsWatch != nil && v2c.ldsWatch.target == lis.GetName() {
		interesting = true
	}
	v2c.mu.Unlock()
	return interesting
}
