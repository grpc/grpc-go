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
	"strings"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"google.golang.org/grpc/internal/grpclog"
)

// UnmarshalRouteConfig processes resources received in an RDS response,
// validates them, and transforms them into a native struct which contains only
// fields we are interested in. The provided hostname determines the route
// configuration resources of interest.
func UnmarshalRouteConfig(resources []*anypb.Any, hostname string, logger *grpclog.PrefixLogger) (map[string]RouteConfigUpdate, error) {
	update := make(map[string]RouteConfigUpdate)
	for _, r := range resources {
		if t := r.GetTypeUrl(); t != V2RouteConfigURL && t != V3RouteConfigURL {
			return nil, fmt.Errorf("xds: unexpected resource type: %s in RDS response", t)
		}
		rc := &v3routepb.RouteConfiguration{}
		if err := proto.Unmarshal(r.GetValue(), rc); err != nil {
			return nil, fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		logger.Infof("Resource with name: %v, type: %T, contains: %v. Picking routes for current watching hostname %v", rc.GetName(), rc, rc, hostname)

		// Use the hostname (resourceName for LDS) to find the routes.
		u, err := generateRDSUpdateFromRouteConfiguration(rc, hostname)
		if err != nil {
			return nil, fmt.Errorf("xds: received invalid RouteConfiguration in RDS response: %+v with err: %v", rc, err)
		}
		update[rc.GetName()] = u
	}
	return update, nil
}

// generateRDSUpdateFromRouteConfiguration checks if the provided
// RouteConfiguration meets the expected criteria. If so, it returns a
// common.RouteConfigUpdate with nil error.
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
func generateRDSUpdateFromRouteConfiguration(rc *v3routepb.RouteConfiguration, host string) (RouteConfigUpdate, error) {
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
	dr := vh.Routes[len(vh.Routes)-1]
	match := dr.GetMatch()
	if match == nil {
		return RouteConfigUpdate{}, fmt.Errorf("matched virtual host's default route doesn't have a match")
	}
	if prefix := match.GetPrefix(); prefix != "" && prefix != "/" {
		// The matched virtual host is invalid. Match is not "" or "/".
		return RouteConfigUpdate{}, fmt.Errorf("matched virtual host's default route is %v, want Prefix empty string or /", match)
	}
	if caseSensitive := match.GetCaseSensitive(); caseSensitive != nil && !caseSensitive.Value {
		// The case sensitive is set to false. Not set or set to true are both
		// valid.
		return RouteConfigUpdate{}, fmt.Errorf("matched virtual host's default route set case-sensitive to false")
	}
	route := dr.GetRoute()
	if route == nil {
		return RouteConfigUpdate{}, fmt.Errorf("matched route is nil")
	}

	if wc := route.GetWeightedClusters(); wc != nil {
		m, err := weightedClustersProtoToMap(wc)
		if err != nil {
			return RouteConfigUpdate{}, fmt.Errorf("matched weighted cluster is invalid: %v", err)
		}
		return RouteConfigUpdate{WeightedCluster: m}, nil
	}

	// When there's just one cluster, we set weightedCluster to map with one
	// entry. This mean we will build a weighted_target balancer even if there's
	// just one cluster.
	//
	// Otherwise, we will need to switch the top policy between weighted_target
	// and CDS. In case when the action changes between one cluster and multiple
	// clusters, changing top level policy means recreating TCP connection every
	// time.
	return RouteConfigUpdate{WeightedCluster: map[string]uint32{route.GetCluster(): 1}}, nil
}

func weightedClustersProtoToMap(wc *v3routepb.WeightedCluster) (map[string]uint32, error) {
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
