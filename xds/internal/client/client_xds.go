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

package client

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	v3clusterpb "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3endpointpb "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	v3listenerpb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	v3typepb "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal"
)

// UnmarshalListener processes resources received in an LDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalListener(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ListenerUpdate, error) {
	update := make(map[string]ListenerUpdate)
	for _, r := range resources {
		if !IsListenerResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in LDS response", r.GetTypeUrl())
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
	if !IsHTTPConnManagerResource(apiLisAny.GetTypeUrl()) {
		return "", fmt.Errorf("xds: unexpected resource type: %q in LDS response", apiLisAny.GetTypeUrl())
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

// UnmarshalRouteConfig processes resources received in an RDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in. The provided hostname determines the route
// configuration resources of interest.
func UnmarshalRouteConfig(resources []*anypb.Any, hostname string, logger *grpclog.PrefixLogger) (map[string]RouteConfigUpdate, error) {
	update := make(map[string]RouteConfigUpdate)
	for _, r := range resources {
		if !IsRouteConfigResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in RDS response", r.GetTypeUrl())
		}
		rc := &v3routepb.RouteConfiguration{}
		if err := proto.Unmarshal(r.GetValue(), rc); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v. Picking routes for current watching hostname %v", rc.GetName(), rc, rc, hostname)

		// Use the hostname (resourceName for LDS) to find the routes.
		u, err := generateRDSUpdateFromRouteConfiguration(rc, hostname, logger)
		if err != nil {
			return nil, fmt.Errorf("xds: received invalid RouteConfiguration in RDS response: %+v with err: %v", rc, err)
		}
		update[rc.GetName()] = u
	}
	return update, nil
}

// generateRDSUpdateFromRouteConfiguration checks if the provided
// RouteConfiguration meets the expected criteria. If so, it returns a
// RouteConfigUpdate with nil error.
//
// A RouteConfiguration resource is considered valid when only if it contains a
// VirtualHost whose domain field matches the server name from the URI passed
// to the gRPC channel, and it contains a clusterName or a weighted cluster.
//
// The RouteConfiguration includes a list of VirtualHosts, which may have zero
// or more elements. We are interested in the element whose domains field
// matches the server name specified in the "xds:" URI. The only field in the
// VirtualHost proto that the we are interested in is the list of routes. We
// only look at the last route in the list (the default route), whose match
// field must be empty and whose route field must be set.  Inside that route
// message, the cluster field will contain the clusterName or weighted clusters
// we are looking for.
func generateRDSUpdateFromRouteConfiguration(rc *v3routepb.RouteConfiguration, host string, logger *grpclog.PrefixLogger) (RouteConfigUpdate, error) {
	//
	// Currently this returns "" on error, and the caller will return an error.
	// But the error doesn't contain details of why the response is invalid
	// (mismatch domain or empty route).
	//
	// For logging purposes, we can log in line. But if we want to populate
	// error details for nack, a detailed error needs to be returned.
	vh := findBestMatchingVirtualHost(host, rc.GetVirtualHosts())
	if vh == nil {
		// No matching virtual host found.
		return RouteConfigUpdate{}, fmt.Errorf("no matching virtual host found")
	}
	if len(vh.Routes) == 0 {
		// The matched virtual host has no routes, this is invalid because there
		// should be at least one default route.
		return RouteConfigUpdate{}, fmt.Errorf("matched virtual host has no routes")
	}

	routes, err := routesProtoToSlice(vh.Routes, logger)
	if err != nil {
		return RouteConfigUpdate{}, fmt.Errorf("received route is invalid: %v", err)
	}
	return RouteConfigUpdate{Routes: routes}, nil
}

func routesProtoToSlice(routes []*v3routepb.Route, logger *grpclog.PrefixLogger) ([]*Route, error) {
	var routesRet []*Route

	for _, r := range routes {
		match := r.GetMatch()
		if match == nil {
			return nil, fmt.Errorf("route %+v doesn't have a match", r)
		}

		if len(match.GetQueryParameters()) != 0 {
			// Ignore route with query parameters.
			logger.Warningf("route %+v has query parameter matchers, the route will be ignored", r)
			continue
		}

		if caseSensitive := match.GetCaseSensitive(); caseSensitive != nil && !caseSensitive.Value {
			return nil, fmt.Errorf("route %+v has case-sensitive false", r)
		}

		pathSp := match.GetPathSpecifier()
		if pathSp == nil {
			return nil, fmt.Errorf("route %+v doesn't have a path specifier", r)
		}

		var route Route
		switch pt := pathSp.(type) {
		case *v3routepb.RouteMatch_Prefix:
			route.Prefix = &pt.Prefix
		case *v3routepb.RouteMatch_Path:
			route.Path = &pt.Path
		case *v3routepb.RouteMatch_SafeRegex:
			route.Regex = &pt.SafeRegex.Regex
		default:
			logger.Warningf("route %+v has an unrecognized path specifier: %+v", r, pt)
			continue
		}

		for _, h := range match.GetHeaders() {
			var header HeaderMatcher
			switch ht := h.GetHeaderMatchSpecifier().(type) {
			case *v3routepb.HeaderMatcher_ExactMatch:
				header.ExactMatch = &ht.ExactMatch
			case *v3routepb.HeaderMatcher_SafeRegexMatch:
				header.RegexMatch = &ht.SafeRegexMatch.Regex
			case *v3routepb.HeaderMatcher_RangeMatch:
				header.RangeMatch = &Int64Range{
					Start: ht.RangeMatch.Start,
					End:   ht.RangeMatch.End,
				}
			case *v3routepb.HeaderMatcher_PresentMatch:
				header.PresentMatch = &ht.PresentMatch
			case *v3routepb.HeaderMatcher_PrefixMatch:
				header.PrefixMatch = &ht.PrefixMatch
			case *v3routepb.HeaderMatcher_SuffixMatch:
				header.SuffixMatch = &ht.SuffixMatch
			default:
				logger.Warningf("route %+v has an unrecognized header matcher: %+v", r, ht)
				continue
			}
			header.Name = h.GetName()
			invert := h.GetInvertMatch()
			header.InvertMatch = &invert
			route.Headers = append(route.Headers, &header)
		}

		if fr := match.GetRuntimeFraction(); fr != nil {
			d := fr.GetDefaultValue()
			n := d.GetNumerator()
			switch d.GetDenominator() {
			case v3typepb.FractionalPercent_HUNDRED:
				n *= 10000
			case v3typepb.FractionalPercent_TEN_THOUSAND:
				n *= 100
			case v3typepb.FractionalPercent_MILLION:
			}
			route.Fraction = &n
		}

		clusters := make(map[string]uint32)
		switch a := r.GetRoute().GetClusterSpecifier().(type) {
		case *v3routepb.RouteAction_Cluster:
			clusters[a.Cluster] = 1
		case *v3routepb.RouteAction_WeightedClusters:
			wcs := a.WeightedClusters
			var totalWeight uint32
			for _, c := range wcs.Clusters {
				w := c.GetWeight().GetValue()
				clusters[c.GetName()] = w
				totalWeight += w
			}
			if totalWeight != wcs.GetTotalWeight().GetValue() {
				return nil, fmt.Errorf("route %+v, action %+v, weights of clusters do not add up to total total weight, got: %v, want %v", r, a, wcs.GetTotalWeight().GetValue(), totalWeight)
			}
		case *v3routepb.RouteAction_ClusterHeader:
			continue
		}

		route.Action = clusters
		routesRet = append(routesRet, &route)
	}
	return routesRet, nil
}

type domainMatchType int

const (
	domainMatchTypeInvalid domainMatchType = iota
	domainMatchTypeUniversal
	domainMatchTypePrefix
	domainMatchTypeSuffix
	domainMatchTypeExact
)

// Exact > Suffix > Prefix > Universal > Invalid.
func (t domainMatchType) betterThan(b domainMatchType) bool {
	return t > b
}

func matchTypeForDomain(d string) domainMatchType {
	if d == "" {
		return domainMatchTypeInvalid
	}
	if d == "*" {
		return domainMatchTypeUniversal
	}
	if strings.HasPrefix(d, "*") {
		return domainMatchTypeSuffix
	}
	if strings.HasSuffix(d, "*") {
		return domainMatchTypePrefix
	}
	if strings.Contains(d, "*") {
		return domainMatchTypeInvalid
	}
	return domainMatchTypeExact
}

func match(domain, host string) (domainMatchType, bool) {
	switch typ := matchTypeForDomain(domain); typ {
	case domainMatchTypeInvalid:
		return typ, false
	case domainMatchTypeUniversal:
		return typ, true
	case domainMatchTypePrefix:
		// abc.*
		return typ, strings.HasPrefix(host, strings.TrimSuffix(domain, "*"))
	case domainMatchTypeSuffix:
		// *.123
		return typ, strings.HasSuffix(host, strings.TrimPrefix(domain, "*"))
	case domainMatchTypeExact:
		return typ, domain == host
	default:
		return domainMatchTypeInvalid, false
	}
}

// findBestMatchingVirtualHost returns the virtual host whose domains field best
// matches host
//
// The domains field support 4 different matching pattern types:
//  - Exact match
//  - Suffix match (e.g. “*ABC”)
//  - Prefix match (e.g. “ABC*)
//  - Universal match (e.g. “*”)
//
// The best match is defined as:
//  - A match is better if it’s matching pattern type is better
//    - Exact match > suffix match > prefix match > universal match
//  - If two matches are of the same pattern type, the longer match is better
//    - This is to compare the length of the matching pattern, e.g. “*ABCDE” >
//    “*ABC”
func findBestMatchingVirtualHost(host string, vHosts []*v3routepb.VirtualHost) *v3routepb.VirtualHost {
	var (
		matchVh   *v3routepb.VirtualHost
		matchType = domainMatchTypeInvalid
		matchLen  int
	)
	for _, vh := range vHosts {
		for _, domain := range vh.GetDomains() {
			typ, matched := match(domain, host)
			if typ == domainMatchTypeInvalid {
				// The rds response is invalid.
				return nil
			}
			if matchType.betterThan(typ) || matchType == typ && matchLen >= len(domain) || !matched {
				// The previous match has better type, or the previous match has
				// better length, or this domain isn't a match.
				continue
			}
			matchVh = vh
			matchType = typ
			matchLen = len(domain)
		}
	}
	return matchVh
}

// UnmarshalCluster processes resources received in an CDS response, validates
// them, and transforms them into a native struct which contains only fields we
// are interested in.
func UnmarshalCluster(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]ClusterUpdate, error) {
	update := make(map[string]ClusterUpdate)
	for _, r := range resources {
		if !IsClusterResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in CDS response", r.GetTypeUrl())
		}

		cluster := &v3clusterpb.Cluster{}
		if err := proto.Unmarshal(r.GetValue(), cluster); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in CDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", cluster.GetName(), cluster, cluster)
		cu, err := validateCluster(cluster)
		if err != nil {
			return nil, err
		}

		// If the Cluster message in the CDS response did not contain a
		// serviceName, we will just use the clusterName for EDS.
		if cu.ServiceName == "" {
			cu.ServiceName = cluster.GetName()
		}
		logger.Debugf("Resource with name %v, value %+v added to cache", cluster.GetName(), cu)
		update[cluster.GetName()] = cu
	}
	return update, nil
}

func validateCluster(cluster *v3clusterpb.Cluster) (ClusterUpdate, error) {
	emptyUpdate := ClusterUpdate{ServiceName: "", EnableLRS: false}
	switch {
	case cluster.GetType() != v3clusterpb.Cluster_EDS:
		return emptyUpdate, fmt.Errorf("xds: unexpected cluster type %v in response: %+v", cluster.GetType(), cluster)
	case cluster.GetEdsClusterConfig().GetEdsConfig().GetAds() == nil:
		return emptyUpdate, fmt.Errorf("xds: unexpected edsConfig in response: %+v", cluster)
	case cluster.GetLbPolicy() != v3clusterpb.Cluster_ROUND_ROBIN:
		return emptyUpdate, fmt.Errorf("xds: unexpected lbPolicy %v in response: %+v", cluster.GetLbPolicy(), cluster)
	}

	return ClusterUpdate{
		ServiceName: cluster.GetEdsClusterConfig().GetServiceName(),
		EnableLRS:   cluster.GetLrsServer().GetSelf() != nil,
	}, nil
}

// UnmarshalEndpoints processes resources received in an EDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in.
func UnmarshalEndpoints(resources []*anypb.Any, logger *grpclog.PrefixLogger) (map[string]EndpointsUpdate, error) {
	update := make(map[string]EndpointsUpdate)
	for _, r := range resources {
		if !IsEndpointsResource(r.GetTypeUrl()) {
			return nil, fmt.Errorf("xds: unexpected resource type: %q in EDS response", r.GetTypeUrl())
		}

		cla := &v3endpointpb.ClusterLoadAssignment{}
		if err := proto.Unmarshal(r.GetValue(), cla); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in EDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v", cla.GetClusterName(), cla, cla)

		u, err := parseEDSRespProto(cla)
		if err != nil {
			return nil, err
		}
		update[cla.GetClusterName()] = u
	}
	return update, nil
}

func parseAddress(socketAddress *v3corepb.SocketAddress) string {
	return net.JoinHostPort(socketAddress.GetAddress(), strconv.Itoa(int(socketAddress.GetPortValue())))
}

func parseDropPolicy(dropPolicy *v3endpointpb.ClusterLoadAssignment_Policy_DropOverload) OverloadDropConfig {
	percentage := dropPolicy.GetDropPercentage()
	var (
		numerator   = percentage.GetNumerator()
		denominator uint32
	)
	switch percentage.GetDenominator() {
	case v3typepb.FractionalPercent_HUNDRED:
		denominator = 100
	case v3typepb.FractionalPercent_TEN_THOUSAND:
		denominator = 10000
	case v3typepb.FractionalPercent_MILLION:
		denominator = 1000000
	}
	return OverloadDropConfig{
		Category:    dropPolicy.GetCategory(),
		Numerator:   numerator,
		Denominator: denominator,
	}
}

func parseEndpoints(lbEndpoints []*v3endpointpb.LbEndpoint) []Endpoint {
	endpoints := make([]Endpoint, 0, len(lbEndpoints))
	for _, lbEndpoint := range lbEndpoints {
		endpoints = append(endpoints, Endpoint{
			HealthStatus: EndpointHealthStatus(lbEndpoint.GetHealthStatus()),
			Address:      parseAddress(lbEndpoint.GetEndpoint().GetAddress().GetSocketAddress()),
			Weight:       lbEndpoint.GetLoadBalancingWeight().GetValue(),
		})
	}
	return endpoints
}

func parseEDSRespProto(m *v3endpointpb.ClusterLoadAssignment) (EndpointsUpdate, error) {
	ret := EndpointsUpdate{}
	for _, dropPolicy := range m.GetPolicy().GetDropOverloads() {
		ret.Drops = append(ret.Drops, parseDropPolicy(dropPolicy))
	}
	priorities := make(map[uint32]struct{})
	for _, locality := range m.Endpoints {
		l := locality.GetLocality()
		if l == nil {
			return EndpointsUpdate{}, fmt.Errorf("EDS response contains a locality without ID, locality: %+v", locality)
		}
		lid := internal.LocalityID{
			Region:  l.Region,
			Zone:    l.Zone,
			SubZone: l.SubZone,
		}
		priority := locality.GetPriority()
		priorities[priority] = struct{}{}
		ret.Localities = append(ret.Localities, Locality{
			ID:        lid,
			Endpoints: parseEndpoints(locality.GetLbEndpoints()),
			Weight:    locality.GetLoadBalancingWeight().GetValue(),
			Priority:  priority,
		})
	}
	for i := 0; i < len(priorities); i++ {
		if _, ok := priorities[uint32(i)]; !ok {
			return EndpointsUpdate{}, fmt.Errorf("priority %v missing (with different priorities %v received)", i, priorities)
		}
	}
	return ret, nil
}
