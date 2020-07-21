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

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	routepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	typepb "github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/internal/grpclog"
)

// handleRDSResponse processes an RDS response received from the xDS server. On
// receipt of a good response, it caches validated resources and also invokes
// the registered watcher callback.
func (v2c *v2Client) handleRDSResponse(resp *xdspb.DiscoveryResponse) error {
	v2c.mu.Lock()
	hostname := v2c.hostname
	v2c.mu.Unlock()

	returnUpdate := make(map[string]rdsUpdate)
	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		rc, ok := resource.Message.(*xdspb.RouteConfiguration)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in RDS response", resource.Message)
		}
		v2c.logger.Infof("Resource with name: %v, type: %T, contains: %v. Picking routes for current watching hostname %v", rc.GetName(), rc, rc, v2c.hostname)

		// Use the hostname (resourceName for LDS) to find the routes.
		u, err := generateRDSUpdateFromRouteConfiguration(rc, hostname, v2c.logger)
		if err != nil {
			return fmt.Errorf("xds: received invalid RouteConfiguration in RDS response: %+v with err: %v", rc, err)
		}
		// If we get here, it means that this resource was a good one.
		returnUpdate[rc.GetName()] = u
	}

	v2c.parent.newRDSUpdate(returnUpdate)
	return nil
}

// generateRDSUpdateFromRouteConfiguration checks if the provided
// RouteConfiguration meets the expected criteria. If so, it returns a rdsUpdate
// with nil error.
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
func generateRDSUpdateFromRouteConfiguration(rc *xdspb.RouteConfiguration, host string, logger *grpclog.PrefixLogger) (rdsUpdate, error) {
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
		return rdsUpdate{}, fmt.Errorf("no matching virtual host found")
	}
	if len(vh.Routes) == 0 {
		// The matched virtual host has no routes, this is invalid because there
		// should be at least one default route.
		return rdsUpdate{}, fmt.Errorf("matched virtual host has no routes")
	}

	routes, err := routesProtoToSlice(vh.Routes, logger)
	if err != nil {
		return rdsUpdate{}, fmt.Errorf("received route is invalid: %v", err)
	}
	return rdsUpdate{routes: routes}, nil
}

func routesProtoToSlice(routes []*routepb.Route, logger *grpclog.PrefixLogger) ([]*Route, error) {
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
		case *routepb.RouteMatch_Prefix:
			route.Prefix = &pt.Prefix
		case *routepb.RouteMatch_Path:
			route.Path = &pt.Path
		case *routepb.RouteMatch_SafeRegex:
			route.Regex = &pt.SafeRegex.Regex
		case *routepb.RouteMatch_Regex:
			return nil, fmt.Errorf("route %+v has Regex, expected SafeRegex instead", r)
		default:
			logger.Warningf("route %+v has an unrecognized path specifier: %+v", r, pt)
			continue
		}

		for _, h := range match.GetHeaders() {
			var header HeaderMatcher
			switch ht := h.GetHeaderMatchSpecifier().(type) {
			case *routepb.HeaderMatcher_ExactMatch:
				header.ExactMatch = &ht.ExactMatch
			case *routepb.HeaderMatcher_SafeRegexMatch:
				header.RegexMatch = &ht.SafeRegexMatch.Regex
			case *routepb.HeaderMatcher_RangeMatch:
				header.RangeMatch = &Int64Range{
					Start: ht.RangeMatch.Start,
					End:   ht.RangeMatch.End,
				}
			case *routepb.HeaderMatcher_PresentMatch:
				header.PresentMatch = &ht.PresentMatch
			case *routepb.HeaderMatcher_PrefixMatch:
				header.PrefixMatch = &ht.PrefixMatch
			case *routepb.HeaderMatcher_SuffixMatch:
				header.SuffixMatch = &ht.SuffixMatch
			case *routepb.HeaderMatcher_RegexMatch:
				return nil, fmt.Errorf("route %+v has a header matcher with Regex, expected SafeRegex instead", r)
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
			case typepb.FractionalPercent_HUNDRED:
				n *= 10000
			case typepb.FractionalPercent_TEN_THOUSAND:
				n *= 100
			case typepb.FractionalPercent_MILLION:
			}
			route.Fraction = &n
		}

		clusters := make(map[string]uint32)
		switch a := r.GetRoute().GetClusterSpecifier().(type) {
		case *routepb.RouteAction_Cluster:
			clusters[a.Cluster] = 1
		case *routepb.RouteAction_WeightedClusters:
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
		case *routepb.RouteAction_ClusterHeader:
			continue
		}

		route.Action = clusters
		routesRet = append(routesRet, &route)
	}
	return routesRet, nil
}

func weightedClustersProtoToMap(wc *routepb.WeightedCluster) (map[string]uint32, error) {
	ret := make(map[string]uint32)
	var totalWeight uint32 = 100
	if t := wc.GetTotalWeight().GetValue(); t != 0 {
		totalWeight = t
	}
	for _, cw := range wc.Clusters {
		w := cw.Weight.GetValue()
		ret[cw.Name] = w
		totalWeight -= w
	}
	if totalWeight != 0 {
		return nil, fmt.Errorf("weights of clusters do not add up to total total weight, difference: %v", totalWeight)
	}
	return ret, nil
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
func findBestMatchingVirtualHost(host string, vHosts []*routepb.VirtualHost) *routepb.VirtualHost {
	var (
		matchVh   *routepb.VirtualHost
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
