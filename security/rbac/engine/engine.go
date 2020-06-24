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

type AuthorizationArgs struct {
	md 			metadata.MD
	peerInfo 	*peer.Peer
}

// Decision is the enum type that represents different authorization
// decisions a CEL engine can return.
type Decision int32

const (
	// DECISION_ALLOW indicates allowing the RPC to go through.
	DECISION_ALLOW Decision = iota
	// DECISION_DENY indicates denying the RPC from going through.
	DECISION_DENY
	// DECISION_UNKNOWN indicates that there is insufficient information to
	// determine whether or not an RPC call is authorized.
	DECISION_UNKNOWN
)

// Output of CEL engine.
type AuthorizationDecision struct {
	decision 				Decision
	authorizationContext	string
}

// Action is the enum type that represents different actions that RBAC
// policies can specify, in the case of a matching context.
type Action int32

const (
	// ACTION_ALLOW indicates that if a policy is matched, authorization
	// can be granted.
	ACTION_ALLOW Action = iota
	// ACTION_DENY indicates that if a policy is matched, authorization
	// will be denied.
	ACTION_DENY
)

// Returns whether or not a policy is matched.
func matches(condition *expr.Expr, args AuthorizationArgs) bool {
	// TODO
	return false
}

// Struct for CEL engine.
type CelEvaluationEngine struct {
	action		Action
	conditions	map[string]*expr.Expr
}

// Builds a CEL evaluation engine from Envoy RBAC.
func RbacToCelEvaluationEngine(rbac envoy_config_rbac_v2.RBAC) CelEvaluationEngine {
	var action Action
	if rbac.Action == envoy_config_rbac_v2.RBAC_ALLOW {
		action = ACTION_ALLOW
	} else {
		action = ACTION_DENY
	}
	
	conditions := make(map[string]*expr.Expr)
	for policyName, policy := range rbac.Policies {
        conditions[policyName] = policy.Condition
	}
	
	return CelEvaluationEngine{action, conditions}
}

// Builds a CEL evaluation engine from runtime policy template.
// Currently do not have access to RTPolicyTemplate/can't import
// func RTPolicyTemplateToCelEvaluationEngine(rt_policy RTPolicyTemplate) CelEvaluationEngine {
// 	// TODO
// 	return nil
// }

// The core function that evaluates whether an RPC is authorized.
func (engine CelEvaluationEngine) evaluate(args AuthorizationArgs) AuthorizationDecision {
	for policyName, condition := range engine.conditions {
		if matches(condition, args) {
			var decision Decision
			if engine.action == ACTION_ALLOW {
				decision = DECISION_ALLOW
			} else {
				decision = DECISION_DENY
			}
			return AuthorizationDecision{decision, "Policy matched: " + policyName}
		}
	}
	// if no conditions matched
	return AuthorizationDecision{DECISION_UNKNOWN, "No policies matched"}
}

func evaluate(args AuthorizationArgs) bool {
	engine := getEngine()
	authDecision := engine.evaluate(args)
	if authDecision.decision == DECISION_DENY {
		return false
	} else if authDecision.decision == DECISION_ALLOW {
		return true
	} else { // DECISION_UNKNOWN
		return false
	}
}
