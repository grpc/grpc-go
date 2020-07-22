// +build go1.10

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
	cel "github.com/google/cel-go/cel"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// AuthorizationArgs is the input of the CEL-based authorization engine.
type AuthorizationArgs struct {
	md       metadata.MD
	peerInfo *peer.Peer
}

// Converts AuthorizationArgs into the evaluation map for CEL.
func (args AuthorizationArgs) toEvalMap() map[string]interface{} {
	// TODO(@ezou): implement the conversion logic.
	return nil
}

// Decision is the enum type that represents different authorization
// decisions a CEL-based authorization engine can return.
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

// String returns the string representation of a Decision object.
func (d Decision) String() string {
	return [...]string{"DecisionAllow", "DecisionDeny", "DecisionUnknown"}[d]
}

// AuthorizationDecision is the output of CEL-based authorization engines.
// If decision is allow or deny, policyNames will either contain the names of
// all the policies matched in the engine that permitted the action, or be
// empty as the decision was made after all conditions evaluated to false.
// If decision is unknown, policyNames will contain the list of policies that
// evaluated to unknown.
type AuthorizationDecision struct {
	decision    Decision
	policyNames []string
}

// Converts an expression to a parsed expression, with SourceInfo nil.
func exprToParsedExpr(condition *expr.Expr) *expr.ParsedExpr {
	return &expr.ParsedExpr{Expr: condition}
}

// Converts an expression to a CEL program.
func exprToProgram(condition *expr.Expr) *cel.Program {
	// TODO(@ezou): implement the conversion from expr to CEL program.
	return nil
}

// Returns whether or not a policy is matched.
func evaluateCondition(condition *cel.Program, evalMap map[string]interface{}) (bool, error) {
	// TODO(@ezou): implement the matching logic using CEL library.
	return false, nil
}

// rbacEngine is the struct for an engine created from one RBAC proto.
type rbacEngine struct {
	action     pb.RBAC_Action
	conditions map[string]*cel.Program
}

// Creates a new rbacEngine from an RBAC.
func newRbacEngine(rbac pb.RBAC) rbacEngine {
	action := rbac.Action
	conditions := make(map[string]*cel.Program)
	for policyName, policy := range rbac.Policies {
		conditions[policyName] = exprToProgram(policy.Condition)
	}
	return rbacEngine{action, conditions}
}

// Returns whether a set of authorization arguments, as contained in evalMap, match an
//  rbacEngine's conditions.
// If any policy matches, the engine has been matched, and the list of matching policy
//  names will be returned.
// Else if any policy is missing attributes, the engine is not matched, and the list of
//  policy names that can't be evaluated due to missing attributes will be returned.
// Else, there is not a match.
func (engine rbacEngine) matches(evalMap map[string]interface{}) (bool, []string) {
	matchingPolicyNames := []string{}
	unknownPolicyNames := []string{}
	for policyName, condition := range engine.conditions {
		match, err := evaluateCondition(condition, evalMap)
		if err != nil {
			unknownPolicyNames = append(unknownPolicyNames, policyName)
		} else if match {
			matchingPolicyNames = append(matchingPolicyNames, policyName)
		}
	}
	if len(matchingPolicyNames) > 0 {
		return true, matchingPolicyNames
	}
	return false, unknownPolicyNames
}

// CelEvaluationEngine is the struct for the CEL-based authorization engine.
type CelEvaluationEngine struct {
	engines []rbacEngine
}

// NewCelEvaluationEngine builds a CEL evaluation engine from a list of Envoy RBACs.
func NewCelEvaluationEngine(rbacs []pb.RBAC) (CelEvaluationEngine, error) {
	if len(rbacs) < 1 || len(rbacs) > 2 {
		return CelEvaluationEngine{}, status.Errorf(codes.InvalidArgument, "must provide 1 or 2 RBACs")
	}
	if len(rbacs) == 2 && (rbacs[0].Action != pb.RBAC_DENY || rbacs[1].Action != pb.RBAC_ALLOW) {
		return CelEvaluationEngine{}, status.Errorf(codes.InvalidArgument, "when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order")
	}
	var engines []rbacEngine
	for _, rbac := range rbacs {
		engines = append(engines, newRbacEngine(rbac))
	}
	return CelEvaluationEngine{engines}, nil
}

// Evaluate is the core function that evaluates whether an RPC is authorized.
//
// ALLOW policy. If one of the RBAC conditions is evaluated as true, then the
// CEL-based authorization engine evaluation returns allow. If all of the RBAC
// conditions are evaluated as false, then it returns deny. Otherwise, some
// conditions are false and some are unknown, it returns undecided.
//
// DENY policy. If one of the RBAC conditions is evaluated as true, then the
// CEL-based authorization engine evaluation returns deny. If all of the RBAC
// conditions are evaluated as false, then it returns allow. Otherwise, some
// conditions are false and some are unknown, it returns undecided.
//
// DENY policy + ALLOW policy. Evaluation is in the following order: If one
// of the expressions in the DENY policy is true, the authorization engine
// returns deny. If one of the expressions in the DENY policy is unknown, it
// returns undecided. Now all the expressions in the DENY policy are false,
// it returns the evaluation of the ALLOW policy.
func (celEngine CelEvaluationEngine) Evaluate(args AuthorizationArgs) (AuthorizationDecision, error) {
	evalMap := args.toEvalMap()
	numEngines := len(celEngine.engines)
	if numEngines < 1 || numEngines > 2 {
		return AuthorizationDecision{}, status.Errorf(codes.Internal, "each CEL-based authorization engine should have 1 or 2 RBAC engines; instead, there are %d in the CEL-based authorization engine provided", numEngines)
	}
	for _, engine := range celEngine.engines {
		match, policyNames := engine.matches(evalMap)
		// If the engine matched, return that engine's action.
		if match {
			if engine.action == pb.RBAC_ALLOW {
				return AuthorizationDecision{DecisionAllow, policyNames}, nil
			}
			return AuthorizationDecision{DecisionDeny, policyNames}, nil
		}
		// If the engine evaluated to unknown, return unknown.
		if len(policyNames) > 0 {
			return AuthorizationDecision{DecisionUnknown, policyNames}, nil
		}
	}
	// If all engines explicitly did not match.
	if len(celEngine.engines) == 1 && celEngine.engines[0].action == pb.RBAC_DENY {
		return AuthorizationDecision{DecisionAllow, []string{}}, nil
	}
	return AuthorizationDecision{DecisionDeny, []string{}}, nil
}
