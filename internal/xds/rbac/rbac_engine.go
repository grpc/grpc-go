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

// Package rbac provides service-level and method-level access control for a
// service.
package rbac

import (
	"net"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// AuthorizationDecision is what will be returned from the RBAC Engine when it
// is asked to see if an incoming RPC matches a policy.
type AuthorizationDecision struct {
	MatchingPolicyName string
}

// Engine is used for making authorization decisions on incoming RPCs.
type Engine struct {
	policies map[string]*policyMatcher
}

// NewEngine creates an RBAC Engine based on the contents of policy. If the
// config is invalid (and fails to build underlying tree of matchers), NewEngine
// will return an error. This created RBAC Engine will not persist the action
// present in the policy, and will leave up to caller to handle the action that
// is attached to the config.
func NewEngine(policy *v3rbacpb.RBAC) (*Engine, error) {
	policies := make(map[string]*policyMatcher)
	for name, config := range policy.Policies {
		matcher, err := newPolicyMatcher(config)
		if err != nil {
			return nil, err
		}
		policies[name] = matcher
	}
	return &Engine{policies: policies}, nil
}

// EvaluateArgs represents the data pulled from an incoming RPC to a gRPC
// server. This data will be used by the RBAC Engine to see if it matches a
// policy this RBAC Engine was configured with or not.
type EvaluateArgs struct {
	// MD is the HTTP Headers that are present in the incoming RPC.
	MD metadata.MD
	// PeerInfo is information about the downstream peer.
	PeerInfo *peer.Peer
	// FullMethod is the method name being called on the upstream service.
	FullMethod string
	// DestinationPort is the port that the RPC is being sent to on the
	// server.
	DestinationPort uint32
	// DestinationAddr is the address that the RPC is being sent to.
	DestinationAddr net.Addr
	// PrincipalName is the name of the downstream principal. If set, the URI
	// SAN or DNS SAN in that order is used from the certificate, otherwise the
	// subject field is used. If unset, it applies to any user that is
	// authenticated.
	PrincipalName string
}

// Evaluate will be called to determine if an incoming RPC matches with a
// policy.
func (r *Engine) Evaluate(args *EvaluateArgs) AuthorizationDecision {
	for policy, matcher := range r.policies {
		if matcher.matches(args) {
			return AuthorizationDecision{
				MatchingPolicyName: policy,
			}
		}
	}
	// TODO: This logic of returning an empty string assumes that the empty
	// string is not a valid policy name. If the empty string is a valid policy
	// name, then we should add to the data returned and have a boolean returned
	// or not.
	return AuthorizationDecision{MatchingPolicyName: ""}
}
