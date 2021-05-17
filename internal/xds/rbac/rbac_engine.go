package rbac

import (
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"net"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
)

// authorizationDecision is what will be returned from the RBAC Engine
// when it is asked to see if an rpc should be allowed or denied.
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
// This data will be used by the RBAC Engine to decide whether to authorize an RPC
// or not.
type EvaluateArgs struct {
	MD metadata.MD // What's in here? I'm assuming this data type can be looked into to get the same fields as those defined in the C struct
	PeerInfo *peer.Peer
	FullMethod string
	DestinationPort uint32 // Will be constructed from listener data
	DestinationAddr net.Addr
}

// Evaluate will be called after the RBAC Engine is instantiated. This will
// determine whether any incoming RPC's are allowed to proceed.

// It seems like Eric is a fan of not returning the type of action and just returning the matching policy name - discuss with Doug in meeting

func (r *RBACEngine) Evaluate(args *EvaluateArgs) AuthorizationDecision {
	// Loop through the policies that this engine was instantiated with. If a policy hits, you now can now return the action
	// this engine was instantiated with, and the policy name. // TODO: is there any precedence on the policy types here?
	// i.e. if there is an admin policy and a policy with any principal as the policy, and the policy with any principal
	// in it hits first, instead of admin, is this okay. I guess, how do we prevent a race condition here lol.
	for policy, matcher := range r.policyMatchers {
		if matcher.matches(args) {
			return AuthorizationDecision{
				//Decision: r.action, // TODO: Is it okay that this is a proto field being returned rather than a type that I defined?
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




// One: look at components,
// copyright, package level comment
// rbac engine higher level component
// matchers: implementation
// RBACEngine, then look at comments
// (RBAC Engine struct)Keep comments to thing that it actually is, except in internal, then you can talk about how it's used
// Document that API only, not HOW it will be used BY SOMEONE ELSE, don't get too precreptiony about how ti's used
// Document WHAT IT IS, NewObject is obvious, RBAC is abbreviation capitaliztion

// Add an error returned, do some validation, returned an error

// evaluateArgs must be exported so caller can set fields

// "used by an rbac engine to authorize rpc"

// can't have authorization.authorizationDecision

// matchingPolicyName: can match with two, or do we expect users to order them - ping Java, C, Jiangtao
// grpclbxds is newer so has threads, focused on xds
// grpc has google3 people as well



// createOrMatcher X, Y, do inline into where it is called