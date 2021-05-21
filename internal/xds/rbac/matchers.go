/*
 * Copyright 2021 gRPC authors.
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
 */

package rbac

import (
	"fmt"
	"net"
	"regexp"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3route_componentspb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	matcher "google.golang.org/grpc/internal/xds/matcher"
)

// TODO: Do we need errors at every node? Should we have checks on each node (i.e. destinationPort should be within a certain range)

// The rbacMatcher interface will be used by the RBAC Engine to help determine
// whether incoming RPC requests should be allowed or denied. There will be many
// types of matchers, each instantiated with part of the policy used to
// instantiate the RBAC Engine. These constructed matchers will form a logical
// tree of matchers, which data about any incoming RPC's will be passed through
// the tree to help make a decision about whether the RPC should be allowed or
// not.
type rbacMatcher interface {
	matches(args *EvaluateArgs) bool
}

// policyMatcher helps determine whether an incoming RPC call matches a policy.
// A policy is a logical role (e.g. Service Admin), which is comprised of
// permissions and principals. A principal is an identity (or identities) for a
// downstream subject which are assigned the policy (role), and a permission is
// an action(s) that a principal(s) can take. A policy matches if both a
// permission and a principal match, which will be determined by the child or
// permissions and principal matchers.
type policyMatcher struct {
	permissions *orMatcher
	principals  *orMatcher
}

func newPolicyMatcher(policy *v3rbacpb.Policy) (*policyMatcher, error) {
	permissions, err := matchersFromPermissions(policy.Permissions)
	if err != nil {
		return nil, err
	}
	principals, err := matchersFromPrincipals(policy.Principals)
	if err != nil {
		return nil, err
	}
	return &policyMatcher{
		permissions: &orMatcher{
			matchers: permissions,
		},
		principals: &orMatcher{
			matchers: principals,
		},
	}, nil
}

func (pm *policyMatcher) matches(args *EvaluateArgs) bool {
	// A policy matches if and only if at least one of its permissions match the
	// action taking place AND at least one if its principals match the
	// downstream peer.
	return pm.permissions.matches(args) && pm.principals.matches(args)
}

// matchersFromPermissions takes a list of permissions (can also be
// a single permission, e.g. from a not rbacMatcher which is logically !permission)
// and returns a list of matchers which correspond to that permission. This will
// be called in many instances throughout the initial construction of the RBAC
// engine from the AND and OR matchers and also from the NOT rbacMatcher.
func matchersFromPermissions(permissions []*v3rbacpb.Permission) ([]rbacMatcher, error) {
	var matchers []rbacMatcher
	for _, permission := range permissions {
		switch permission.GetRule().(type) {
		case *v3rbacpb.Permission_AndRules:
			mList, err := matchersFromPermissions(permission.GetAndRules().Rules)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &andMatcher{matchers: mList})
		case *v3rbacpb.Permission_OrRules:
			mList, err := matchersFromPermissions(permission.GetOrRules().Rules)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &orMatcher{
				matchers: mList,
			})
		case *v3rbacpb.Permission_Any:
			matchers = append(matchers, &alwaysMatcher{})
		case *v3rbacpb.Permission_Header:
			m, err := newHeaderMatcher(permission.GetHeader())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Permission_UrlPath:
			m, err := newURLPathMatcher(permission.GetUrlPath())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Permission_DestinationIp:
			m, err := newDestinationIPMatcher(permission.GetDestinationIp())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Permission_DestinationPort:
			matchers = append(matchers, newPortMatcher(permission.GetDestinationPort()))
		case *v3rbacpb.Permission_NotRule:
			mList, err := matchersFromPermissions([]*v3rbacpb.Permission{permission})
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &notMatcher{
				matcherToNot: mList[0],
			})
		case *v3rbacpb.Permission_Metadata:
			// Not supported in gRPC RBAC currently - a permission typed as
			// Metadata in the initial config will be a no-op.
		case *v3rbacpb.Permission_RequestedServerName:
			// Not supported in gRPC RBAC currently - a permission typed as
			// requested server name in the initial config will be a no-op.
		}
	}
	return matchers, nil
}

func matchersFromPrincipals(principals []*v3rbacpb.Principal) ([]rbacMatcher, error) {
	var matchers []rbacMatcher
	for _, principal := range principals {
		switch principal.GetIdentifier().(type) {
		case *v3rbacpb.Principal_AndIds:
			mList, err := matchersFromPrincipals(principal.GetAndIds().Ids)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &andMatcher{matchers: mList})
		case *v3rbacpb.Principal_OrIds:
			mList, err := matchersFromPrincipals(principal.GetOrIds().Ids)
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &orMatcher{matchers: mList})
		case *v3rbacpb.Principal_Any:
			matchers = append(matchers, &alwaysMatcher{})
		case *v3rbacpb.Principal_Authenticated_:
			authenticatedMatcher, err := newAuthenticatedMatcher(principal.GetAuthenticated())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, authenticatedMatcher)
		case *v3rbacpb.Principal_DirectRemoteIp:
			m, err := newSourceIPMatcher(principal.GetDirectRemoteIp())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Principal_Header:
			// Do we need an error here?
			m, err := newHeaderMatcher(principal.GetHeader())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Principal_UrlPath:
			m, err := newURLPathMatcher(principal.GetUrlPath())
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, m)
		case *v3rbacpb.Principal_NotId:
			mList, err := matchersFromPrincipals([]*v3rbacpb.Principal{principal})
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, &notMatcher{
				matcherToNot: mList[0],
			})
		case *v3rbacpb.Principal_SourceIp:
			// The source ip principal identifier is deprecated. Thus, a
			// principal typed as a source ip in the identifier will be a no-op.
			// The config should use DirectRemoteIp instead.
		case *v3rbacpb.Principal_RemoteIp:
			// Not supported in gRPC RBAC currently - a principal typed as
			// Remote Ip in the initial config will be a no-op.
		case *v3rbacpb.Principal_Metadata:
			// Not supported in gRPC RBAC currently - a principal typed as
			// Metadata in the initial config will be a no-op.
		}
	}
	return matchers, nil
}

// orMatcher is a rbacMatcher where it successfully matches if one of it's children
// successfully match. It also logically represents a principal or permission,
// but can also be it's own entity further down the tree of matchers.
type orMatcher struct {
	matchers []rbacMatcher
}

func (om *orMatcher) matches(args *EvaluateArgs) bool {
	// Range through child matchers and pass in rbacData, and only one child
	// rbacMatcher has to match to be logically successful.
	for _, matcher := range om.matchers {
		if matcher.matches(args) {
			return true
		}
	}
	return false
}

// andMatcher is a rbacMatcher that is successful if every child rbacMatcher matches.
type andMatcher struct {
	matchers []rbacMatcher
}

func (am *andMatcher) matches(args *EvaluateArgs) bool {
	for _, matcher := range am.matchers {
		if !matcher.matches(args) {
			return false
		}
	}
	return true
}

// alwaysMatcher is a rbacMatcher that will always match. This logically represents
// an any rule for a permission or a principal.
type alwaysMatcher struct {
}

func (am *alwaysMatcher) matches(args *EvaluateArgs) bool {
	return true
}

// notMatcher is a rbacMatcher that nots an underlying rbacMatcher.
type notMatcher struct {
	matcherToNot rbacMatcher
}

func (nm *notMatcher) matches(args *EvaluateArgs) bool {
	return !nm.matcherToNot.matches(args)
}

// headerMatcher is a rbacMatcher that matches on incoming HTTP Headers present in the incoming RPC.
type headerMatcher struct {
	headerMatcherInterface matcher.HeaderMatcherInterface
}

// TODO: Do you need errors on every node? Especially this one, where it doesn't
// seem like error conditions can even arise?
func newHeaderMatcher(headerMatcherConfig *v3route_componentspb.HeaderMatcher) (*headerMatcher, error) {
	var headerMatcherInterface matcher.HeaderMatcherInterface
	switch headerMatcherConfig.HeaderMatchSpecifier.(type) {
	case *v3route_componentspb.HeaderMatcher_ExactMatch:
		headerMatcherInterface = matcher.NewHeaderExactMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetExactMatch())
	case *v3route_componentspb.HeaderMatcher_SafeRegexMatch:
		// What to do about panic logic?
		headerMatcherInterface = matcher.NewHeaderRegexMatcher(headerMatcherConfig.Name, regexp.MustCompile(headerMatcherConfig.GetSafeRegexMatch().Regex))
	case *v3route_componentspb.HeaderMatcher_RangeMatch:
		headerMatcherInterface = matcher.NewHeaderRangeMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetRangeMatch().Start, headerMatcherConfig.GetRangeMatch().End)
	case *v3route_componentspb.HeaderMatcher_PresentMatch:
		headerMatcherInterface = matcher.NewHeaderPresentMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetPresentMatch())
	case *v3route_componentspb.HeaderMatcher_PrefixMatch:
		headerMatcherInterface = matcher.NewHeaderPrefixMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetPrefixMatch())
	case *v3route_componentspb.HeaderMatcher_SuffixMatch:
		headerMatcherInterface = matcher.NewHeaderSuffixMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetSuffixMatch())
	case *v3route_componentspb.HeaderMatcher_ContainsMatch:
		headerMatcherInterface = matcher.NewHeaderContainsMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetContainsMatch())
	}
	if headerMatcherConfig.InvertMatch {
		headerMatcherInterface = matcher.NewInvertMatcher(headerMatcherInterface)
	}
	return &headerMatcher{
		headerMatcherInterface: headerMatcherInterface,
	}, nil
}

func (hm *headerMatcher) matches(args *EvaluateArgs) bool {
	return hm.headerMatcherInterface.Match(args.MD)
}

// urlPathMatcher matches on the URL Path of the incoming RPC. In gRPC, this
// logically maps to the full method name the RPC is calling on the server side.
type urlPathMatcher struct {
	stringMatcher matcher.StringMatcher
}

func newURLPathMatcher(pathMatcher *v3matcherpb.PathMatcher) (*urlPathMatcher, error) {
	// This string rbacMatcher does become prefix match
	stringMatcher, err := matcher.StringMatcherFromProto(pathMatcher.GetPath())
	if err != nil {
		return nil, err
	}
	return &urlPathMatcher{
		stringMatcher: stringMatcher,
	}, nil
}

func (upm *urlPathMatcher) matches(args *EvaluateArgs) bool {
	return upm.stringMatcher.Match(args.FullMethod)
}

// sourceIPMatcher and destinationIPMatcher both are matchers that match against
// a CIDR Range. Two different matchers are needed as the source and ip address
// come from different parts of the data about incoming RPC's passed in.
// Matching a CIDR Range means to determine whether the IP Address falls within
// the CIDR Range or not.
type sourceIPMatcher struct {
	// ipNet represents the CidrRange that this rbacMatcher was configured with.
	// This is what will source and destination IP's will be matched against.
	ipNet *net.IPNet
}

func newSourceIPMatcher(cidrRange *v3corepb.CidrRange) (*sourceIPMatcher, error) {
	// Convert configuration to a cidrRangeString, as Go standard library has
	// methods that parse cidr string.
	cidrRangeString := fmt.Sprintf("%s/%d", cidrRange.AddressPrefix, cidrRange.PrefixLen.Value)
	_, ipNet, err := net.ParseCIDR(cidrRangeString)
	if err != nil {
		return nil, err
	}
	return &sourceIPMatcher{
		ipNet: ipNet,
	}, nil
}

func (sim *sourceIPMatcher) matches(args *EvaluateArgs) bool {
	return sim.ipNet.Contains(net.IP(args.PeerInfo.Addr.String()))
}

type destinationIPMatcher struct {
	ipNet *net.IPNet
}

func newDestinationIPMatcher(cidrRange *v3corepb.CidrRange) (*destinationIPMatcher, error) {
	cidrRangeString := cidrRange.AddressPrefix + fmt.Sprint(cidrRange)
	_, ipNet, err := net.ParseCIDR(cidrRangeString)
	if err != nil {
		return nil, err
	}
	return &destinationIPMatcher{
		ipNet: ipNet,
	}, nil
}

func (dim *destinationIPMatcher) matches(args *EvaluateArgs) bool {
	return dim.ipNet.Contains(net.IP(args.DestinationAddr.String()))
}

// portMatcher matches on whether the destination port of the RPC matches the
// destination port this rbacMatcher was instantiated with.
type portMatcher struct {
	destinationPort uint32
}

func newPortMatcher(destinationPort uint32) *portMatcher {
	return &portMatcher{
		destinationPort: destinationPort,
	}
}

func (pm *portMatcher) matches(args *EvaluateArgs) bool {
	return args.DestinationPort == pm.destinationPort
}

// authenticatedMatcher matches on the name of the Principal. If set, the URI
// SAN or DNS SAN in that order is used from the certificate, otherwise the
// subject field is used. If unset, it applies to any user that is
// authenticated.
type authenticatedMatcher struct {
	stringMatcher matcher.StringMatcher
}

func newAuthenticatedMatcher(authenticatedMatcherConfig *v3rbacpb.Principal_Authenticated) (*authenticatedMatcher, error) {
	stringMatcher, err := matcher.StringMatcherFromProto(authenticatedMatcherConfig.PrincipalName)
	if err != nil {
		return nil, err
	}
	return &authenticatedMatcher{
		stringMatcher: stringMatcher,
	}, nil
}

func (am *authenticatedMatcher) matches(args *EvaluateArgs) bool {
	return am.stringMatcher.Match(args.PrincipalName)
}
