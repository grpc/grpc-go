/*
 *
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

// Package engine contains functions/structs for CEL engine.
package engine

import (
	rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	v1alpha1 "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// struct that holds attribute context
type EvaluateArgs struct {
	// md metadata.MD
	// conn net.Conn
	// auth credentials.AuthInfo
}

// Decision enum
type Decision int32

const (
	DECISION_ALLOW		Decision = 0
	DECISION_DENY  		Decision = 1
	DECISION_UNKNOWN	Decision = 2
)

var Decision_name = map[int32]string{
	0: "ALLOW",
	1: "DENY",
	2: "UNKNOWN",
}

var Decision_value = map[string]int32{
	"ALLOW": 	0,
	"DENY":  	1,
	"UNKNOWN": 	2,
}

// output of CEL engine
type AuthorizationDecision struct {
	decision 				Decision
	authorizationContext	string
}

// Action enum
type Action int32

const (
	ACTION_ALLOW	Action = 0
	ACTION_DENY  	Action = 1
)

var Action_name = map[int32]string{
	0: "ALLOW",
	1: "DENY",
}

var Action_value = map[string]int32{
	"ALLOW": 	0,
	"DENY":  	1,
}

// struct for CEL expressions
type Condition struct {
	expr	*v1alpha1.Expr
}

func (condition Condition) Matches(args EvaluateArgs) bool {
	// TODO
	return false
}

// struct for CEL engine
type CelEvaluationEngine struct {
	action		Action
	conditions	map[string]Condition
}

// Builds a CEL evaluation engine from Envoy RBAC.
func RbacToCelEvaluationEngine(r rbac.RBAC) CelEvaluationEngine {
	var action Action
	if r.Action == rbac.RBAC_ALLOW {
		action = ACTION_ALLOW
	} else {
		action = ACTION_DENY
	}
	
	conditions := make(map[string]Condition)
	for policyName, policy := range r.Policies {
        conditions[policyName] = Condition{policy.Condition}
	}
	
	return CelEvaluationEngine{action, conditions}
}

// Builds a CEL evaluation engine from runtime policy template.
// Currently do not have access to RTPolicyTemplate/can't import
// func RTPolicyTemplateToCelEvaluationEngine(rt_policy RTPolicyTemplate) CelEvaluationEngine {
// 	// TODO
// 	return nil
// }

// The core function that evaluates whether an RPC is authorized
func (engine CelEvaluationEngine) Evaluate(args EvaluateArgs) AuthorizationDecision {
	for policyName, condition := range engine.conditions {
		if condition.Matches(args) {
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
