package rbac

import (
"fmt"
	matcher2 "google.golang.org/grpc/internal/xds/matcher"
	"net"
	"regexp"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
v3route_componentspb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// Logically,

// (*********) gets passed around to a logical tree of matchers with and rules or rules etc.
// This is the logical tree.

// A policy is defined as logically matching both a permission and a principal, which are both or matchers

// The matcher interface will be used by the RBAC Engine to help determine whether incoming RPC requests should
// be allowed or denied. There will be many types of matchers, each instantiated with part of the policy used to
// instantiate the RBAC Engine. These constructed matchers will form a logical tree of matchers, which data about
// any incoming RPC's will be passed through the tree to help make a decision about whether the RPC should be allowed
// or not.
type matcher interface {
	matches(args *EvaluateArgs) bool
}
/*
// TODO: Should the matcher interface have another method defined it called createMatcher? This seems illogical, as
// each logical matcher node is configured with a different section of the config.
func createMatcher() matcher {
	// "Return different derived Matchers based on the permission rule types, ex. return an and matcher for kAndRulesz
	// a path matcher for url path, etc."
}
*/

// policyMatcher helps determine whether an incoming RPC call matches a policy.
// A policy is a logical role (e.g. Service Admin), which is comprised of
// permissions and principals. A principal is an identity (or identities) for a
// downstream subject which are assigned the policy (role), and a permission is an
// action(s) that a principal(s) can take. A policy matches if both a permission
// and a principal match, which will be determined by the child or permissions and
// principal matchers.

type policyMatcher struct {
	permissions *orMatcher
	principals *orMatcher
}

func newPolicyMatcher(policy *v3rbacpb.Policy) *policyMatcher {
	return &policyMatcher{
		permissions: &orMatcher{
			matchers: createMatcherListFromPermissionList(policy.Permissions),
		},
		principals: &orMatcher{
			matchers: createMatcherListFromPrincipalList(policy.Principals),
		},
	}
}


func (pm *policyMatcher) matches(args *EvaluateArgs) bool {
	// Due to a policy matching iff one of the permissions match the action taking place and one of the principals
	// match the peer, you can simply delegate the data about the incoming RPC to the child permission and principal or matchers.
	return pm.permissions.matches(args) && pm.principals.matches(args)
}


// If it's not a pointer it'll be a lot of copies
// createMatcherFromPermissionList takes a permission list (can also be a single permission, for example in a not matcher, which
// is !permission) and returns a matcher than corresponds to that permission.

// createMatcherListFromPermissionList takes a list of permissions (can also be a single permission, e.g. from a not matcher
// which is logically !permission) and returns a list of matchers which correspond to that permission. This will be called
// in many instances throughout the initial construction of the RBAC engine from the AND and OR matchers and also from
// the NOT matcher.
func createMatcherListFromPermissionList(permissions []*v3rbacpb.Permission) []matcher {
	var matcherList []matcher
	for _, permission := range permissions {
		switch permission.GetRule().(type) {
		case *v3rbacpb.Permission_AndRules:
			matcherList = append(matcherList, &andMatcher{matchers: createMatcherListFromPermissionList(permission.GetAndRules().Rules)})
		case *v3rbacpb.Permission_OrRules:
			matcherList = append(matcherList, &orMatcher{
				matchers: createMatcherListFromPermissionList(permission.GetOrRules().Rules),
			})
		case *v3rbacpb.Permission_Any:
			matcherList = append(matcherList, &alwaysMatcher{})
		case *v3rbacpb.Permission_Header:
			matcherList = append(matcherList, newHeaderMatcher(permission.GetHeader()))
		case *v3rbacpb.Permission_UrlPath:
			matcherList = append(matcherList, newUrlPathMatcher(permission.GetUrlPath()))
		case *v3rbacpb.Permission_DestinationIp:
			matcherList = append(matcherList, newDestinationIpMatcher(permission.GetDestinationIp()))
		case *v3rbacpb.Permission_DestinationPort:
			matcherList = append(matcherList, newPortMatcher(permission.GetDestinationPort()))
		case *v3rbacpb.Permission_Metadata:
			// Not supported in gRPC RBAC currently - a permission typed as Metadata in the initial config will be a no-op.
		case *v3rbacpb.Permission_NotRule:
			matcherList = append(matcherList, &notMatcher{
			matcherToNot: createMatcherListFromPermissionList([]*v3rbacpb.Permission{permission})[0],
			})
			//matcherList = append(matcherList, newNotMatcherPermission(permission))
		case *v3rbacpb.Permission_RequestedServerName:
			// Not supported in gRPC RBAC currently - a permission typed as requested server name in the initial config will
			// be a no-op.
		}
	}
	return matcherList
}

func createMatcherListFromPrincipalList(principals []*v3rbacpb.Principal) []matcher {
	var matcherList []matcher
	for _, principal := range principals {
		switch principal.GetIdentifier().(type) {
		case *v3rbacpb.Principal_AndIds:
			matcherList = append(matcherList, &andMatcher{matchers: createMatcherListFromPrincipalList(principal.GetAndIds().Ids)}) // Make this generic have it as matchers
		case *v3rbacpb.Principal_OrIds:
			matcherList = append(matcherList, &orMatcher{matchers: createMatcherListFromPrincipalList(principal.GetOrIds().Ids)})
		case *v3rbacpb.Principal_Any:
			matcherList = append(matcherList, &alwaysMatcher{})
		case *v3rbacpb.Principal_Authenticated_:
			// What matcher do I put here lol? - looks like this is only new one
			newAuthenticatedMatcher(principal.GetAuthenticated())
			/*principal.GetAuthenticated().
			matcherList = append(matcherList, &authenticatedMatcher{
				stringMatcher: matcher2.NewStri
			})*/
		case *v3rbacpb.Principal_SourceIp: // This is logically distinct from destination ip and thus will need a seperate matcher type, as matches will call the peer info rather than passed in from listener.
			matcherList = append(matcherList, newSourceIpMatcher(principal.GetSourceIp())) // TODO: What to do about this deprecated field here?
		case *v3rbacpb.Principal_DirectRemoteIp: // This is the same thing as source ip
			matcherList = append(matcherList, newSourceIpMatcher(principal.GetDirectRemoteIp()))
		case *v3rbacpb.Principal_RemoteIp:
			// Not supported in gRPC RBAC currently - a principal typed as Remote Ip in the initial config will be a no-op.
		case *v3rbacpb.Principal_Header:
			matcherList = append(matcherList, newHeaderMatcher(principal.GetHeader()))
		case *v3rbacpb.Principal_UrlPath:
			matcherList = append(matcherList, newUrlPathMatcher(principal.GetUrlPath()))
		case *v3rbacpb.Principal_Metadata:
			// Not supported in gRPC RBAC currently - a principal typed as Metadata in the initial config will be a no-op.
		case *v3rbacpb.Principal_NotId:
			//matcherList = append(matcherList, newNotMatcherPrincipal(principal))
			matcherList = append(matcherList, &notMatcher{
				matcherToNot: createMatcherListFromPrincipalList([]*v3rbacpb.Principal{principal})[0],
			})
		}
	}
	return matcherList
}

// orMatcher is a matcher where it successfully matches if one of it's children successfully match.
// It also logically represents a principal or permission, but can also be it's own entity further down
// the config tree.
type orMatcher struct {
	matchers []matcher // You can create this inline rather than what I have
}
/*
func newOrMatcherPermissions(permissions []*v3rbacpb.Permission) *orMatcher { // Generic concept or, base level combinatorial and or not, base level branching, indistingushable
	return &orMatcher{
		matchers: createMatcherListFromPermissionList(permissions),
	}
}

func newOrMatcherPrincipals(principals []*v3rbacpb.Principal) *orMatcher {
	return &orMatcher{
		matchers: createMatcherListFromPrincipalList(principals),
	}
}*/

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
 // Lol I forgot to get rid of these
func newNotMatcherPermission(permission *v3rbacpb.Permission) *notMatcher {
	// The Cardinality of the matcher list to the permission list will be 1 to 1.
	matcherList := createMatcherListFromPermissionList([]*v3rbacpb.Permission{permission})
	return &notMatcher{
		matcherToNot: matcherList[0],
	}
}


func (nm *notMatcher) matches(args *EvaluateArgs) bool {
	return !nm.matcherToNot.matches(args)
}

func newNotMatcherPrincipal(principal *v3rbacpb.Principal) *notMatcher {
	// The cardinality of the matcher list to the policy list will be 1 to 1.
	matcherList := createMatcherListFromPrincipalList([]*v3rbacpb.Principal{principal})
	return &notMatcher{
		matcherToNot: matcherList[0],
	}
}






// The four types of matchers still left to implement are

// header Matcher

// url path Matcher

// ip Matcher

// port Matcher


// headerMatcher will be a wrapper around the headerMatchers already in codebase (in xds resolver, which I plan to move to internal so it can be shared). The config will determine
// the type, and this struct will persist a matcher (determined by config), to pass in Metadata too.
type headerMatcher struct {
	// headerMatcher headerMatcherInterface (will be moved to internal
	 headerMatcherInterface matcher2.HeaderMatcherInterface // NO POINTERS FOR INTERFACES
}

func newHeaderMatcher(headerMatcherConfig *v3route_componentspb.HeaderMatcher) *headerMatcher {
	// TODO: What to do about InvertMatch?

	// Convert that HeaderMatcher type from function argument to the
	// soon to be internal headerMatcherInterface.
	// Branch across the type of this headerMatcher, instantiate an internal matcher interface type
	// take evaluateArgs, look into it for Metadata field, then pass that to the internal matcher interface.
	// Take that config, branch across type, then you persist ONE of the internal Black boxes that I will move
	// One of across header matcher type
	// This switch will determine what headerMatcher to instantiate
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
	// If there's an invert matcher present in config, handle it here with the header matcher interface
	if headerMatcherConfig.InvertMatch {
		headerMatcherInterface = matcher2.NewInvertMatcher(headerMatcherInterface)
	}
	return &headerMatcher{
		headerMatcherInterface: headerMatcherInterface,
	}
}

func (hm *headerMatcher) matches(args *EvaluateArgs) bool {
	// Use that persisted internal black box, pull metadata from args function argument, then send that to
	// the internal black box for the function argument.
	return hm.headerMatcherInterface.Match(args.MD)
}


type urlPathMatcher struct {
	// What state do you need here? (Will get converted from this v3matcherpb thing)
	// This state could also be a matcher you pull from xds/internal/resolver/... into internal
	stringMatcher matcher2.StringMatcher
}

func newUrlPathMatcher(pathMatcher *v3matcherpb.PathMatcher) *urlPathMatcher {
	// There's a path matcher in matcher_path.go in same directory as xds resolver.
	// match(path string), exact, prefix, regex match
	// This gets into string matcher branching logic, which the 6 types are defined as: exact, prefix, suffix, safe regex
	// contains, HiddenEnvoyDeprecatedRegex
	stringMatcher, err := matcher2.StringMatcherFromProto(pathMatcher.GetPath())

	// Handle errors here.

	return &urlPathMatcher{
		stringMatcher: stringMatcher,
	}
}

func (upm *urlPathMatcher) matches(args *EvaluateArgs) bool {
	return upm.stringMatcher.Match(args.FullMethod)
}

// sourceIpMatcher and destinationIpMatcher both are matchers that match against
// a CIDR Range. Two different matchers are needed as the source and ip address
// come from different parts of the data passed in. Matching a CIDR Range means
// to determine whether the IP Address falls within the CIDR Range or not.

type sourceIpMatcher struct {
	// ipNet represents the CidrRange that this matcher was configured with.
	// This is what will source and destination IP's will be matched against.
	ipNet *net.IPNet

}

func newSourceIpMatcher(cidrRange *v3corepb.CidrRange) *sourceIpMatcher {
	// Convert configuration to a cidrRangeString, as Go standard library has methods that parse
	// cidr string.
	cidrRangeString := cidrRange.AddressPrefix + fmt.Sprint(cidrRange.PrefixLen.Value) // Does go prefer () or just calling the object within the proto object?
	_, ipNet, err := net.ParseCIDR(cidrRangeString) // What to do about error handling? BIG QUESTION
	// Error handling here.
	return &sourceIpMatcher{
		ipNet: ipNet,
	}
}

func (sim *sourceIpMatcher) matches(args *EvaluateArgs) bool {
	return sim.ipNet.Contains(net.IP(args.PeerInfo.Addr.String()))
}


type destinationIpMatcher struct {
	ipNet *net.IPNet
}

func newDestinationIpMatcher(cidrRange *v3corepb.CidrRange) *destinationIpMatcher {
	cidrRangeString := cidrRange.AddressPrefix + fmt.Sprint(cidrRange)
	_, ipNet, err := net.ParseCIDR(cidrRangeString) // Again, big question of error handling
	// Error handling here.
	return &destinationIpMatcher{
		ipNet :ipNet,
	}
}

func (dim *destinationIpMatcher) matches(args *EvaluateArgs) bool {
	return dim.ipNet.Contains(net.IP(args.DestinationAddr.String()))
}


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


// Authenticated Matcher?


type authenticatedMatcher struct {
	// String matcher
	stringMatcher matcher2.StringMatcher // You could also just create this inline
}

func newAuthenticatedMatcher(authenticatedMatcherConfig *v3rbacpb.Principal_Authenticated) *authenticatedMatcher {
	stringMatcher, err := matcher2.StringMatcherFromProto(authenticatedMatcherConfig.PrincipalName)

	// Error handling here.

	return &authenticatedMatcher{
		stringMatcher: stringMatcher,
	}
}

func (am *authenticatedMatcher) matches(args EvaluateArgs) {
	// TODO: Figure out what to compare to stringMatcher
	// The name of the principal. If set, the URI SAN or DNS SAN in that order is used from
	// the certificate, otherwise the subject field is used. If unset, it applies to any user that is authenticated.
	args.PeerInfo.AuthInfo
	args.PeerInfo.AuthInfo.AuthType()

}