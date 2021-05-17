package rbac

import (
	"net"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// authorizationDecision is what will be returned from the RBAC Engine
// when it is asked to see if an rpc matches a policy.
type AuthorizationDecision struct {
	//Decision v3rbacpb.RBAC_Action
	MatchingPolicyName string
}

// RBACEngine is used for making authorization decisions on an incoming
// RPC.
type RBACEngine struct {
	//action v3rbacpb.RBAC_Action
	policyMatchers map[string]*policyMatcher
}

// NewRBACEngine will be used in order to create a Rbac Engine
// based on a policy. This policy will be used to instantiate a tree
// of matchers that will be used to make an authorization decision on
// an incoming RPC.
func NewRBACEngine(policy *v3rbacpb.RBAC) (*RBACEngine, error) {
	var policyMatchers map[string]*policyMatcher
	for policyName, policyConfig := range policy.Policies {
		policyMatcher, err := newPolicyMatcher(policyConfig)
		if err != nil {
			return nil, err
		}
		policyMatchers[policyName] = policyMatcher
	}
	return &RBACEngine{
		//action: policy.Action,
		policyMatchers: policyMatchers,
	}, nil
}

// evaluateArgs represents the data pulled from an incoming RPC to a gRPC server.
// This data will be used by the RBAC Engine to see if it matches a policy this
// RBAC Engine was configured with or not.
type EvaluateArgs struct {
	MD              metadata.MD
	PeerInfo        *peer.Peer
	FullMethod      string
	DestinationPort uint32
	DestinationAddr net.Addr
}

// Evaluate will be called after the RBAC Engine is instantiated. This will
// see if an incoming RPC matches with a policy or not.
func (r *RBACEngine) Evaluate(args *EvaluateArgs) AuthorizationDecision {
	for policy, matcher := range r.policyMatchers {
		if matcher.matches(args) {
			return AuthorizationDecision{
				//Decision: r.action,
				MatchingPolicyName: policy,
			}
		}
	}
	// TODO: This logic of returning an empty string assumes that the empty string is not a valid policy name. If the empty
	// string is a valid policy name, then we should add to the data returned and have a boolean returned or not.
	return AuthorizationDecision{
		MatchingPolicyName: "",
	}
}
