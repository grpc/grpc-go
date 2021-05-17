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
	matcher2 "google.golang.org/grpc/internal/xds/matcher"
)

// TODO: Do we need errors at every node? Should we have checks on each node (i.e. destinationPort should be within a certain range)

// The matcher interface will be used by the RBAC Engine to help determine whether incoming RPC requests should
// be allowed or denied. There will be many types of matchers, each instantiated with part of the policy used to
// instantiate the RBAC Engine. These constructed matchers will form a logical tree of matchers, which data about
// any incoming RPC's will be passed through the tree to help make a decision about whether the RPC should be allowed
// or not.
type matcher interface {
	matches(args *EvaluateArgs) bool
}

// policyMatcher helps determine whether an incoming RPC call matches a policy.
// A policy is a logical role (e.g. Service Admin), which is comprised of
// permissions and principals. A principal is an identity (or identities) for a
// downstream subject which are assigned the policy (role), and a permission is an
// action(s) that a principal(s) can take. A policy matches if both a permission
// and a principal match, which will be determined by the child or permissions and
// principal matchers.

type policyMatcher struct {
	permissions *orMatcher
	principals  *orMatcher
}

func newPolicyMatcher(policy *v3rbacpb.Policy) (*policyMatcher, error) {
	permissionsList, err := createMatcherListFromPermissionList(policy.Permissions)
	if err != nil {
		return nil, err
	}
	principalsList, err := createMatcherListFromPrincipalList(policy.Principals)
	if err != nil {
		return nil, err
	}
	return &policyMatcher{
		permissions: &orMatcher{
			matchers: permissionsList,
		},
		principals: &orMatcher{
			matchers: principalsList,
		},
	}, nil
}

func (pm *policyMatcher) matches(args *EvaluateArgs) bool {
	// Due to a policy matching iff one of the permissions match the action taking place and one of the principals
	// match the peer, you can simply delegate the data about the incoming RPC to the child permission and principal or matchers.
	return pm.permissions.matches(args) && pm.principals.matches(args)
}

// createMatcherListFromPermissionList takes a list of permissions (can also be a single permission, e.g. from a not matcher
// which is logically !permission) and returns a list of matchers which correspond to that permission. This will be called
// in many instances throughout the initial construction of the RBAC engine from the AND and OR matchers and also from
// the NOT matcher.
func createMatcherListFromPermissionList(permissions []*v3rbacpb.Permission) ([]matcher, error) {
	var matcherList []matcher
	for _, permission := range permissions {
		switch permission.GetRule().(type) {
		case *v3rbacpb.Permission_AndRules:
			andPermissionList, err := createMatcherListFromPermissionList(permission.GetAndRules().Rules)
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &andMatcher{matchers: andPermissionList})
		case *v3rbacpb.Permission_OrRules:
			orPermissionList, err := createMatcherListFromPermissionList(permission.GetOrRules().Rules)
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &orMatcher{
				matchers: orPermissionList,
			})
		case *v3rbacpb.Permission_Any:
			matcherList = append(matcherList, &alwaysMatcher{})
		case *v3rbacpb.Permission_Header:
			headerMatcher, err := newHeaderMatcher(permission.GetHeader())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, headerMatcher)
		case *v3rbacpb.Permission_UrlPath:
			urlPathMatcher, err := newUrlPathMatcher(permission.GetUrlPath())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, urlPathMatcher)
		case *v3rbacpb.Permission_DestinationIp:
			destinationMatcher, err := newDestinationIpMatcher(permission.GetDestinationIp())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, destinationMatcher)
		case *v3rbacpb.Permission_DestinationPort:
			matcherList = append(matcherList, newPortMatcher(permission.GetDestinationPort()))
		case *v3rbacpb.Permission_Metadata:
			// Not supported in gRPC RBAC currently - a permission typed as Metadata in the initial config will be a no-op.
		case *v3rbacpb.Permission_NotRule:
			matcherToNot, err := createMatcherListFromPermissionList([]*v3rbacpb.Permission{permission})
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &notMatcher{
				matcherToNot: matcherToNot[0],
			})
		case *v3rbacpb.Permission_RequestedServerName:
			// Not supported in gRPC RBAC currently - a permission typed as requested server name in the initial config will
			// be a no-op.
		}
	}
	return matcherList, nil
}

func createMatcherListFromPrincipalList(principals []*v3rbacpb.Principal) ([]matcher, error) {
	var matcherList []matcher
	for _, principal := range principals {
		switch principal.GetIdentifier().(type) {
		case *v3rbacpb.Principal_AndIds:
			andMatcherList, err := createMatcherListFromPrincipalList(principal.GetAndIds().Ids)
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &andMatcher{matchers: andMatcherList}) // Make this generic have it as matchers
		case *v3rbacpb.Principal_OrIds:
			orMatcherList, err := createMatcherListFromPrincipalList(principal.GetOrIds().Ids)
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &orMatcher{matchers: orMatcherList})
		case *v3rbacpb.Principal_Any:
			matcherList = append(matcherList, &alwaysMatcher{})
		case *v3rbacpb.Principal_Authenticated_:
			authenticatedMatcher, err := newAuthenticatedMatcher(principal.GetAuthenticated())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, authenticatedMatcher) // Do this inline?
		case *v3rbacpb.Principal_SourceIp: // This is logically distinct from destination ip and thus will need a seperate matcher type, as matches will call the peer info rather than passed in from listener.
			// Perhaps say "No-op, deprecated, if present in config return error"
			matcherList = append(matcherList, newSourceIpMatcher(principal.GetSourceIp())) // TODO: What to do about this deprecated field here?
		case *v3rbacpb.Principal_DirectRemoteIp: // This is the same thing as source ip
			sourceIpMatcher, err := newSourceIpMatcher(principal.GetDirectRemoteIp())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, sourceIpMatcher)
		case *v3rbacpb.Principal_RemoteIp:
			// Not supported in gRPC RBAC currently - a principal typed as Remote Ip in the initial config will be a no-op.
		case *v3rbacpb.Principal_Header:
			// Do we need an error here?
			headerMatcher, err := newHeaderMatcher(principal.GetHeader())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, headerMatcher)
		case *v3rbacpb.Principal_UrlPath:
			urlPathMatcher, err := newUrlPathMatcher(principal.GetUrlPath())
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, urlPathMatcher)
		case *v3rbacpb.Principal_Metadata:
			// Not supported in gRPC RBAC currently - a principal typed as Metadata in the initial config will be a no-op.
		case *v3rbacpb.Principal_NotId:
			matcherToNot, err := createMatcherListFromPrincipalList([]*v3rbacpb.Principal{principal})
			if err != nil {
				return nil, err
			}
			matcherList = append(matcherList, &notMatcher{
				matcherToNot: matcherToNot[0],
			})
		}
	}
	return matcherList, nil
}

// orMatcher is a matcher where it successfully matches if one of it's children successfully match.
// It also logically represents a principal or permission, but can also be it's own entity further down
// the tree of matchers.
type orMatcher struct {
	matchers []matcher
}

func (om *orMatcher) matches(args *EvaluateArgs) bool {
	// Range through child matchers and pass in rbacData, and only one child matcher has to match to be
	// logically successful.
	for _, matcher := range om.matchers {
		if matcher.matches(args) {
			return true
		}
	}
	return false
}

// andMatcher is a matcher that is successful if every child matcher matches.
type andMatcher struct {
	matchers []matcher
}

func (am *andMatcher) matches(args *EvaluateArgs) bool {
	for _, matcher := range am.matchers {
		if !matcher.matches(args) {
			return false
		}
	}
	return true
}

// alwaysMatcher is a matcher that will always match. This logically represents an any rule for a permission or a principal.
type alwaysMatcher struct {
}

func (am *alwaysMatcher) matches(args *EvaluateArgs) bool {
	return true
}

// notMatcher is a matcher that nots an underlying matcher.
type notMatcher struct {
	matcherToNot matcher
}

func (nm *notMatcher) matches(args *EvaluateArgs) bool {
	return !nm.matcherToNot.matches(args)
}

// headerMatcher is a matcher that matches on incoming HTTP Headers present in the incoming RPC.
type headerMatcher struct {
	headerMatcherInterface matcher2.HeaderMatcherInterface
}

// TODO: Do you need errors on every node? Especially this one, where it doesn't seem like error conditions can even arise?
// If so, find any error conditions that could arise and add return them.
func newHeaderMatcher(headerMatcherConfig *v3route_componentspb.HeaderMatcher) (*headerMatcher, error) {
	var headerMatcherInterface matcher2.HeaderMatcherInterface
	switch headerMatcherConfig.HeaderMatchSpecifier.(type) {
	case *v3route_componentspb.HeaderMatcher_ExactMatch:
		headerMatcherInterface = matcher2.NewHeaderExactMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetExactMatch())
	case *v3route_componentspb.HeaderMatcher_SafeRegexMatch:
		// What to do about panic logic?
		headerMatcherInterface = matcher2.NewHeaderRegexMatcher(headerMatcherConfig.Name, regexp.MustCompile(headerMatcherConfig.GetSafeRegexMatch().Regex))
	case *v3route_componentspb.HeaderMatcher_RangeMatch:
		headerMatcherInterface = matcher2.NewHeaderRangeMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetRangeMatch().Start, headerMatcherConfig.GetRangeMatch().End)
	case *v3route_componentspb.HeaderMatcher_PresentMatch:
		headerMatcherInterface = matcher2.NewHeaderPresentMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetPresentMatch())
	case *v3route_componentspb.HeaderMatcher_PrefixMatch:
		headerMatcherInterface = matcher2.NewHeaderPrefixMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetPrefixMatch())
	case *v3route_componentspb.HeaderMatcher_SuffixMatch:
		headerMatcherInterface = matcher2.NewHeaderSuffixMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetSuffixMatch())
	case *v3route_componentspb.HeaderMatcher_ContainsMatch:
		headerMatcherInterface = matcher2.NewHeaderContainsMatcher(headerMatcherConfig.Name, headerMatcherConfig.GetContainsMatch())
	}
	if headerMatcherConfig.InvertMatch {
		headerMatcherInterface = matcher2.NewInvertMatcher(headerMatcherInterface)
	}
	return &headerMatcher{
		headerMatcherInterface: headerMatcherInterface,
	}, nil
}

func (hm *headerMatcher) matches(args *EvaluateArgs) bool {
	return hm.headerMatcherInterface.Match(args.MD)
}

// urlPathMatcher matches on the URL Path of the incoming RPC. In gRPC, this logically maps
// to the full method name the RPC is calling on the server side.
type urlPathMatcher struct {
	stringMatcher matcher2.StringMatcher
}

func newUrlPathMatcher(pathMatcher *v3matcherpb.PathMatcher) (*urlPathMatcher, error) {
	stringMatcher, err := matcher2.StringMatcherFromProto(pathMatcher.GetPath())
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

// sourceIpMatcher and destinationIpMatcher both are matchers that match against
// a CIDR Range. Two different matchers are needed as the source and ip address
// come from different parts of the data about incoming RPC's passed in.
// Matching a CIDR Range means to determine whether the IP Address falls within
// the CIDR Range or not.

type sourceIpMatcher struct {
	// ipNet represents the CidrRange that this matcher was configured with.
	// This is what will source and destination IP's will be matched against.
	ipNet *net.IPNet
}

func newSourceIpMatcher(cidrRange *v3corepb.CidrRange) (*sourceIpMatcher, error) {
	// Convert configuration to a cidrRangeString, as Go standard library has methods that parse
	// cidr string.
	cidrRangeString := cidrRange.AddressPrefix + fmt.Sprint(cidrRange.PrefixLen.Value)
	_, ipNet, err := net.ParseCIDR(cidrRangeString)
	if err != nil {
		return nil, err
	}
	return &sourceIpMatcher{
		ipNet: ipNet,
	}, nil
}

func (sim *sourceIpMatcher) matches(args *EvaluateArgs) bool {
	return sim.ipNet.Contains(net.IP(args.PeerInfo.Addr.String()))
}

type destinationIpMatcher struct {
	ipNet *net.IPNet
}

func newDestinationIpMatcher(cidrRange *v3corepb.CidrRange) (*destinationIpMatcher, error) {
	cidrRangeString := cidrRange.AddressPrefix + fmt.Sprint(cidrRange)
	_, ipNet, err := net.ParseCIDR(cidrRangeString)
	if err != nil {
		return nil, err
	}
	return &destinationIpMatcher{
		ipNet: ipNet,
	}, nil
}

func (dim *destinationIpMatcher) matches(args *EvaluateArgs) bool {
	return dim.ipNet.Contains(net.IP(args.DestinationAddr.String()))
}

// portMatcher matches on whether the destination port of the RPC
// matches the destination port this matcher was instantiated with
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

// authenticatedMatcher matches on the name of the Principal. If set, the URI SAN or DNS SAN in that order
// is used from the certificate, otherwise the subject field is used. If unset, it applies to any user
// that is authenticated.
type authenticatedMatcher struct {
	stringMatcher matcher2.StringMatcher
}

func newAuthenticatedMatcher(authenticatedMatcherConfig *v3rbacpb.Principal_Authenticated) (*authenticatedMatcher, error) {
	stringMatcher, err := matcher2.StringMatcherFromProto(authenticatedMatcherConfig.PrincipalName)
	if err != nil {
		return nil, err
	}
	return &authenticatedMatcher{
		stringMatcher: stringMatcher,
	}, nil
}

func (am *authenticatedMatcher) matches(args *EvaluateArgs) bool {
	// TODO: Figure out what to compare to stringMatcher
	// The name of the principal. If set, the URI SAN or DNS SAN in that order is used from
	// the certificate, otherwise the subject field is used. If unset, it applies to any user that is authenticated.
	args.PeerInfo.AuthInfo
	args.PeerInfo.AuthInfo.AuthType()

}
