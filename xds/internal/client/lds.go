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

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	httppb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
)

// newLDSRequest generates an LDS request proto for the provided target, to be
// sent out on the wire.
func (v2c *v2Client) newLDSRequest(target []string) *xdspb.DiscoveryRequest {
	return &xdspb.DiscoveryRequest{
		Node:          v2c.nodeProto,
		TypeUrl:       listenerURL,
		ResourceNames: target,
	}
}

// sendLDS sends an LDS request for provided target on the provided stream.
func (v2c *v2Client) sendLDS(stream adsStream, target []string) bool {
	if err := stream.Send(v2c.newLDSRequest(target)); err != nil {
		grpclog.Warningf("xds: LDS request for resource %v failed: %v", target, err)
		return false
	}
	return true
}

// handleLDSResponse processes an LDS response received from the xDS server. It
// performs the following actions:
// - sanity checks the received message (only the listener we are watching for)
// - extracts the RDS related information
// - invokes the registered watcher callback with the appropriate update or
//   error if the received message does not contain a listener for the watch
//   target.
//
// Returns error to the caller only on marshaling errors or if the listener
// that we are interested in has problems in the response.
func (v2c *v2Client) handleLDSResponse(resp *xdspb.DiscoveryResponse) error {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	routeName := ""
	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in LDS response: %v", err)
		}
		lis, ok := resource.Message.(*xdspb.Listener)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in LDS response", resource.Message)
		}
		if !v2c.isListenerProtoInteresting(lis) {
			// We ignore listeners we are not watching for because LDS is
			// special in the sense that there is only one resource we are
			// interested in, and this resource does not change over the
			// lifetime of the v2Client. So, we don't have to cache other
			// listeners which we are not interested in.
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
			if apiLis.GetRds().GetRouteConfigName() == "" {
				return fmt.Errorf("xds: empty route_config_name in LDS response: %+v", lis)
			}
			routeName = apiLis.GetRds().GetRouteConfigName()
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

	if wi := v2c.watchMap[ldsResource]; wi != nil {
		var err error
		if routeName == "" {
			err = fmt.Errorf("xds: LDS target %s not found in received response %+v", wi.target, resp)
		}
		// Type assert the callback field to the appropriate callback type and
		// invoke it.
		wi.callback.(ldsCallback)(ldsUpdate{routeName: routeName}, err)
	}
	return nil
}

// isListenerProtoInteresting determines if the provided listener matches the
// target that we are watching for.
//
// Caller should hold v2c.mu
func (v2c *v2Client) isListenerProtoInteresting(lis *xdspb.Listener) bool {
	return v2c.watchMap[ldsResource] != nil && v2c.watchMap[ldsResource].target[0] == lis.GetName()
}
