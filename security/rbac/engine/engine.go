/*
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
 */

package engine

import (
	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// AuthorizationArgs is the input of CEL engine.
type AuthorizationArgs struct {
	md       metadata.MD
	peerInfo *peer.Peer
}

// Decision is the enum type that represents different authorization
// decisions a CEL engine can return.
type Decision int32

const (
	// DecisionAllow indicates allowing the RPC to go through.
	DecisionAllow Decision = iota
	// DecisionDeny indicates denying the RPC from going through.
	DecisionDeny
	// DecisionUnknown indicates that there is insufficient information to
	// determine whether or not an RPC call is authorized.
	DecisionUnknown
)

// AuthorizationDecision is the output of CEL engine.
type AuthorizationDecision struct {
	decision             Decision
	authorizationContext string
}

// Returns whether or not a policy is matched.
func matches(condition *expr.Expr, args AuthorizationArgs) bool {
	// TODO
	return false
}

// CelEvaluationEngine is the struct for CEL engine.
type CelEvaluationEngine struct {
	action     pb.RBAC_Action
	conditions map[string]*expr.Expr
}

// NewCelEvaluationEngine builds a CEL evaluation engine from Envoy RBAC.
func NewCelEvaluationEngine(rbac pb.RBAC) CelEvaluationEngine {
	action := rbac.Action
	conditions := make(map[string]*expr.Expr)
	for policyName, policy := range rbac.Policies {
		conditions[policyName] = policy.Condition
	}
	return CelEvaluationEngine{action, conditions}
}

// NewCelEvaluationEngine builds a CEL evaluation engine from runtime policy template.
// Currently do not have access to RTPolicyTemplate/can't import
// func NewCelEvaluationEngine(rt_policy RTPolicyTemplate) CelEvaluationEngine {
// 	// TODO
// 	return nil
// }

// Evaluate is the core function that evaluates whether an RPC is authorized.
func (engine CelEvaluationEngine) Evaluate(args AuthorizationArgs) AuthorizationDecision {
	for policyName, condition := range engine.conditions {
		if matches(condition, args) {
			var decision Decision
			if engine.action == pb.RBAC_ALLOW {
				decision = DecisionAllow
			} else {
				decision = DecisionDeny
			}
			return AuthorizationDecision{decision, "Policy matched: " + policyName}
		}
	}
	// if no conditions matched
	return AuthorizationDecision{DecisionUnknown, "No policies matched"}
}
