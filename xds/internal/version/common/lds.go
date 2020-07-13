/*
 *
 * Copyright 2020 gRPC authors.
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

package common

import (
	"fmt"

	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/internal/grpclog"
)

// UnmarshalListener processes resources received in an LDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalListener(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ListenerUpdate, error) {
	update := make(map[string]ListenerUpdate)
	for _, r := range resources {
		if t := r.GetTypeUrl(); t != V2ListenerURL && t != V3ListenerURL {
			return nil, fmt.Errorf("xds: unexpected resource type: %s in LDS response", t)
		}
		lis := &v3listenerpb.Listener{}
		if err := proto.Unmarshal(r.GetValue(), lis); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in LDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", lis.GetName(), lis, lis)
		routeName, err := getRouteConfigNameFromListener(lis, logger)
		if err != nil {
			return nil, err
		}
		update[lis.GetName()] = ListenerUpdate{RouteConfigName: routeName}
	}
	return update, nil
}

// getRouteConfigNameFromListener checks if the provided Listener proto meets
// the expected criteria. If so, it returns a non-empty routeConfigName.
func getRouteConfigNameFromListener(lis *v3listenerpb.Listener, logger *grpclog.PrefixLogger) (string, error) {
	if lis.GetApiListener() == nil {
		return "", fmt.Errorf("xds: no api_listener field in LDS response %+v", lis)
	}
	apiLisAny := lis.GetApiListener().GetApiListener()
	if t := apiLisAny.GetTypeUrl(); t != V3HTTPConnManagerURL && t != V2HTTPConnManagerURL {
		return "", fmt.Errorf("xds: unexpected resource type: %s in LDS response", t)
	}
	apiLis := &v3httppb.HttpConnectionManager{}
	if err := proto.Unmarshal(apiLisAny.GetValue(), apiLis); err != nil {
		return "", fmt.Errorf("xds: failed to unmarshal api_listner in LDS response: %v", err)
	}

	logger.Infof("Resource with type %T, contains %v", apiLis, apiLis)
	switch apiLis.RouteSpecifier.(type) {
	case *v3httppb.HttpConnectionManager_Rds:
		if apiLis.GetRds().GetConfigSource().GetAds() == nil {
			return "", fmt.Errorf("xds: ConfigSource is not ADS in LDS response: %+v", lis)
		}
		name := apiLis.GetRds().GetRouteConfigName()
		if name == "" {
			return "", fmt.Errorf("xds: empty route_config_name in LDS response: %+v", lis)
		}
		return name, nil
	case *v3httppb.HttpConnectionManager_RouteConfig:
		// TODO: Add support for specifying the RouteConfiguration inline
		// in the LDS response.
		return "", fmt.Errorf("xds: LDS response contains RDS config inline. Not supported for now: %+v", apiLis)
	case nil:
		return "", fmt.Errorf("xds: no RouteSpecifier in received LDS response: %+v", apiLis)
	default:
		return "", fmt.Errorf("xds: unsupported type %T for RouteSpecifier in received LDS response", apiLis.RouteSpecifier)
	}
}
