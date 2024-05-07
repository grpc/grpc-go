// Copyright 2018 Envoyproxy Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"google.golang.org/protobuf/proto"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	runtime "github.com/envoyproxy/go-control-plane/envoy/service/runtime/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/types"
	"github.com/envoyproxy/go-control-plane/pkg/resource/v3"
)

// GetResponseType returns the enumeration for a valid xDS type URL.
func GetResponseType(typeURL resource.Type) types.ResponseType {
	switch typeURL {
	case resource.EndpointType:
		return types.Endpoint
	case resource.ClusterType:
		return types.Cluster
	case resource.RouteType:
		return types.Route
	case resource.ScopedRouteType:
		return types.ScopedRoute
	case resource.VirtualHostType:
		return types.VirtualHost
	case resource.ListenerType:
		return types.Listener
	case resource.SecretType:
		return types.Secret
	case resource.RuntimeType:
		return types.Runtime
	case resource.ExtensionConfigType:
		return types.ExtensionConfig
	case resource.RateLimitConfigType:
		return types.RateLimitConfig
	}
	return types.UnknownType
}

// GetResponseTypeURL returns the type url for a valid enum.
func GetResponseTypeURL(responseType types.ResponseType) (string, error) {
	switch responseType {
	case types.Endpoint:
		return resource.EndpointType, nil
	case types.Cluster:
		return resource.ClusterType, nil
	case types.Route:
		return resource.RouteType, nil
	case types.ScopedRoute:
		return resource.ScopedRouteType, nil
	case types.VirtualHost:
		return resource.VirtualHostType, nil
	case types.Listener:
		return resource.ListenerType, nil
	case types.Secret:
		return resource.SecretType, nil
	case types.Runtime:
		return resource.RuntimeType, nil
	case types.ExtensionConfig:
		return resource.ExtensionConfigType, nil
	case types.RateLimitConfig:
		return resource.RateLimitConfigType, nil
	default:
		return "", fmt.Errorf("couldn't map response type %v to known resource type", responseType)
	}
}

// GetResourceName returns the resource name for a valid xDS response type.
func GetResourceName(res types.Resource) string {
	switch v := res.(type) {
	case *endpoint.ClusterLoadAssignment:
		return v.GetClusterName()
	case types.ResourceWithName:
		return v.GetName()
	default:
		return ""
	}
}

// GetResourceName returns the resource names for a list of valid xDS response types.
func GetResourceNames(resources []types.Resource) []string {
	out := make([]string, len(resources))
	for i, r := range resources {
		out[i] = GetResourceName(r)
	}
	return out
}

// MarshalResource converts the Resource to MarshaledResource.
func MarshalResource(resource types.Resource) (types.MarshaledResource, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(resource)
}

// GetResourceReferences returns a map of dependent resources keyed by resource type, given a map of resources.
// (EDS cluster names for CDS, RDS/SRDS routes names for LDS, RDS route names for SRDS).
func GetResourceReferences(resources map[string]types.ResourceWithTTL) map[resource.Type]map[string]bool {
	out := make(map[resource.Type]map[string]bool)
	getResourceReferences(resources, out)

	return out
}

// GetAllResourceReferences returns a map of dependent resources keyed by resources type, given all resources.
func GetAllResourceReferences(resourceGroups [types.UnknownType]Resources) map[resource.Type]map[string]bool {
	ret := map[resource.Type]map[string]bool{}

	// We only check resources that we expect to have references to other resources.
	responseTypesWithReferences := map[types.ResponseType]struct{}{
		types.Cluster:     {},
		types.Listener:    {},
		types.ScopedRoute: {},
	}

	for responseType, resourceGroup := range resourceGroups {
		if _, ok := responseTypesWithReferences[types.ResponseType(responseType)]; ok {
			items := resourceGroup.Items
			getResourceReferences(items, ret)
		}
	}

	return ret
}

func getResourceReferences(resources map[string]types.ResourceWithTTL, out map[resource.Type]map[string]bool) {
	for _, res := range resources {
		if res.Resource == nil {
			continue
		}

		switch v := res.Resource.(type) {
		case *endpoint.ClusterLoadAssignment:
			// No dependencies.
		case *cluster.Cluster:
			getClusterReferences(v, out)
		case *route.RouteConfiguration:
			// References to clusters in both routes (and listeners) are not included
			// in the result, because the clusters are retrieved in bulk currently,
			// and not by name.
		case *route.ScopedRouteConfiguration:
			getScopedRouteReferences(v, out)
		case *listener.Listener:
			getListenerReferences(v, out)
		case *runtime.Runtime:
			// no dependencies
		}
	}
}

func mapMerge(dst map[string]bool, src map[string]bool) {
	for k, v := range src {
		dst[k] = v
	}
}

// Clusters will reference either the endpoint's cluster name or ServiceName override.
func getClusterReferences(src *cluster.Cluster, out map[resource.Type]map[string]bool) {
	endpoints := map[string]bool{}

	switch typ := src.GetClusterDiscoveryType().(type) {
	case *cluster.Cluster_Type:
		if typ.Type == cluster.Cluster_EDS {
			if src.GetEdsClusterConfig() != nil && src.GetEdsClusterConfig().GetServiceName() != "" {
				endpoints[src.GetEdsClusterConfig().GetServiceName()] = true
			} else {
				endpoints[src.GetName()] = true
			}
		}
	}

	if len(endpoints) > 0 {
		if _, ok := out[resource.EndpointType]; !ok {
			out[resource.EndpointType] = map[string]bool{}
		}

		mapMerge(out[resource.EndpointType], endpoints)
	}
}

// HTTP listeners will either reference ScopedRoutes or Routes.
func getListenerReferences(src *listener.Listener, out map[resource.Type]map[string]bool) {
	routes := map[string]bool{}

	// Extract route configuration names from HTTP connection manager.
	for _, chain := range src.GetFilterChains() {
		for _, filter := range chain.GetFilters() {
			config := resource.GetHTTPConnectionManager(filter)
			if config == nil {
				continue
			}

			// If we are using RDS, add the referenced the route name.
			if name := config.GetRds().GetRouteConfigName(); name != "" {
				routes[name] = true
			}

			// If the scoped route mapping is embedded, add the referenced route resource names.
			for _, s := range config.GetScopedRoutes().GetScopedRouteConfigurationsList().GetScopedRouteConfigurations() {
				routes[s.GetRouteConfigurationName()] = true
			}
		}
	}

	if len(routes) > 0 {
		if _, ok := out[resource.RouteType]; !ok {
			out[resource.RouteType] = map[string]bool{}
		}

		mapMerge(out[resource.RouteType], routes)
	}
}

func getScopedRouteReferences(src *route.ScopedRouteConfiguration, out map[resource.Type]map[string]bool) {
	routes := map[string]bool{}

	// For a scoped route configuration, the dependent resource is the RouteConfigurationName.
	routes[src.GetRouteConfigurationName()] = true

	if len(routes) > 0 {
		if _, ok := out[resource.RouteType]; !ok {
			out[resource.RouteType] = map[string]bool{}
		}

		mapMerge(out[resource.RouteType], routes)
	}
}

// HashResource will take a resource and create a SHA256 hash sum out of the marshaled bytes
func HashResource(resource []byte) string {
	hasher := sha256.New()
	hasher.Write(resource)

	return hex.EncodeToString(hasher.Sum(nil))
}
