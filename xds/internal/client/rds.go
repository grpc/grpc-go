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
	"github.com/golang/protobuf/ptypes"
)

// handleRDSResponse processes an RDS response received from the xDS server. On
// receipt of a good response, it caches validated resources and also invokes
// the registered watcher callback.
func (v2c *v2Client) handleRDSResponse(resp *xdspb.DiscoveryResponse) error {
	v2c.mu.Lock()
	defer v2c.mu.Unlock()

	if v2c.watchMap[ldsURL] == nil {
		return fmt.Errorf("xds: unexpected RDS response when no LDS watcher is registered: %+v", resp)
	}
	target := v2c.watchMap[ldsURL].target[0]

	wi := v2c.watchMap[rdsURL]
	if wi == nil {
		return fmt.Errorf("xds: no RDS watcher found when handling RDS response: %+v", resp)
	}

	returnCluster := ""
	localCache := make(map[string]string)
	for _, r := range resp.GetResources() {
		var resource ptypes.DynamicAny
		if err := ptypes.UnmarshalAny(r, &resource); err != nil {
			return fmt.Errorf("xds: failed to unmarshal resource in RDS response: %v", err)
		}
		rc, ok := resource.Message.(*xdspb.RouteConfiguration)
		if !ok {
			return fmt.Errorf("xds: unexpected resource type: %T in RDS response", resource.Message)
		}
		v2c.logger.Infof("Resource with name: %v, type: %T, contains: %v", rc.GetName(), rc, rc)
		cluster := getClusterFromRouteConfiguration(rc, target)
		if cluster == "" {
			return fmt.Errorf("xds: received invalid RouteConfiguration in RDS response: %+v", rc)
		}

		// If we get here, it means that this resource was a good one.
		localCache[rc.GetName()] = cluster
		v2c.logger.Debugf("Resource with name %v, type %T, value %+v added to cache", rc.GetName(), cluster, cluster)

		// TODO: remove cache, and only process resources that are interesting.
		if rc.GetName() == wi.target[0] {
			returnCluster = cluster
		}
	}

	// Update the cache in the v2Client only after we have confirmed that all
	// resources in the received response were good.
	for k, v := range localCache {
		// TODO: Need to handle deletion of entries from the cache based on LDS
		// watch calls. Not handling it does not affect correctness, but leads
		// to unnecessary memory consumption.
		v2c.rdsCache[k] = v
	}

	if returnCluster != "" {
		// We stop the expiry timer and invoke the callback only when we have
		// received the resource that we are watching for. Since RDS is an
		// incremental protocol, the fact that we did not receive the resource
		// that we are watching for in this response does not mean that the
		// server does not know about it.
		wi.stopTimer()
		wi.rdsCallback(rdsUpdate{clusterName: returnCluster}, nil)
	}
	return nil
}

// getClusterFromRouteConfiguration checks if the provided RouteConfiguration
// meets the expected criteria. If so, it returns a non-empty clusterName.
//
// A RouteConfiguration resource is considered valid when only if it contains a
// VirtualHost whose domain field matches the server name from the URI passed
// to the gRPC channel, and it contains a clusterName.
//
// The RouteConfiguration includes a list of VirtualHosts, which may have zero
// or more elements. We are interested in the element whose domains field
// matches the server name specified in the "xds:" URI. The only field in the
// VirtualHost proto that the we are interested in is the list of routes. We
// only look at the last route in the list (the default route), whose match
// field must be empty and whose route field must be set.  Inside that route
// message, the cluster field will contain the clusterName we are looking for.
func getClusterFromRouteConfiguration(rc *xdspb.RouteConfiguration, host string) string {
	// TODO: return error for better error logging and nack.
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
		return ""
	}
	if len(vh.Routes) == 0 {
		// The matched virtual host has no routes, this is invalid because there
		// should be at least one default route.
		return ""
	}
	dr := vh.Routes[len(vh.Routes)-1]
	if match := dr.GetMatch(); match == nil || match.GetPrefix() != "" {
		// The matched virtual host is invalid.
		return ""
	}
	if route := dr.GetRoute(); route != nil {
		return route.GetCluster()
	}
	return ""
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
