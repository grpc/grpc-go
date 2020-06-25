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
	"github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// Input of CEL engine.
type AuthorizationArgs struct {
	md 			metadata.MD
	peerInfo 	*peer.Peer
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

// Output of CEL engine.
type AuthorizationDecision struct {
	decision 				Decision
	authorizationContext	string
}

// Returns whether or not a policy is matched.
func matches(condition *expr.Expr, args AuthorizationArgs) bool {
	// TODO
	return false
}

// Struct for CEL engine.
type celEvaluationEngine struct {
	action		envoy_config_rbac_v2.RBAC_Action
	conditions	map[string]*expr.Expr
}

// Builds a CEL evaluation engine from Envoy RBAC.
func NewCelEvaluationEngine(rbac envoy_config_rbac_v2.RBAC) celEvaluationEngine {
	action := rbac.Action
	conditions := make(map[string]*expr.Expr)
	for policyName, policy := range rbac.Policies {
        conditions[policyName] = policy.Condition
	}
	return celEvaluationEngine{action, conditions}
}

// Builds a CEL evaluation engine from runtime policy template.
// Currently do not have access to RTPolicyTemplate/can't import
// func NewCelEvaluationEngine(rt_policy RTPolicyTemplate) celEvaluationEngine {
// 	// TODO
// 	return nil
// }

// The core function that evaluates whether an RPC is authorized.
func (engine celEvaluationEngine) Evaluate(args AuthorizationArgs) AuthorizationDecision {
	for policyName, condition := range engine.conditions {
		if matches(condition, args) {
			var decision Decision
			if engine.action == envoy_config_rbac_v2.RBAC_ALLOW {
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
