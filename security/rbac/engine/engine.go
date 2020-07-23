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

// Evaluates a CEL expression. Returns true if CEL evaluation returns true. Returns false if
// CEL evaluation returns false. Returns an error if CEL evaluation returns unknown or an
// unsuccessful evaluation.
func evaluateCondition(condition *cel.Program, evalMap map[string]interface{}) (bool, error) {
	// TODO(@ezou): implement the matching logic using CEL library.
	return false, nil
}

// policyEngine is the struct for an engine created from one RBAC proto.
type policyEngine struct {
	action     pb.RBAC_Action
	conditions map[string]*cel.Program
}

// Creates a new policyEngine from an RBAC policy proto.
func newPolicyEngine(rbac pb.RBAC) policyEngine {
	action := rbac.Action
	conditions := make(map[string]*cel.Program)
	for policyName, policy := range rbac.Policies {
		conditions[policyName] = exprToProgram(policy.Condition)
	}
	return policyEngine{action, conditions}
}

// Returns the decision of an engine based on whether or not AuthorizationArgs is a match,
// i.e. if engine's action is ALLOW and match is true, we will return DecisionAllow;
// if engine's action is ALLOW and match is false, we will return DecisionDeny.
func getDecision(engine policyEngine, match bool) Decision {
	if engine.action == pb.RBAC_ALLOW && match || engine.action == pb.RBAC_DENY && !match {
		return DecisionAllow
	}
	return DecisionDeny
}

// Returns the authorization decision of a single policy engine based on evalMap.
// If any policy matches, the decision matches the engine's action, and the list of
//  matching policy names will be returned.
// Else if any policy is missing attributes, the decision is unknown, and the list of
//  policy names that can't be evaluated due to missing attributes will be returned.
// Else, the decision is the opposite of the engine's action, i.e. an ALLOW engine
//  will return DecisionDeny, and vice versa.
func (engine policyEngine) evaluate(evalMap map[string]interface{}) (Decision, []string) {
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
		return getDecision(engine, true), matchingPolicyNames
	}
	if len(unknownPolicyNames) > 0 {
		return DecisionUnknown, unknownPolicyNames
	}
	return getDecision(engine, false), []string{}
}

// AuthorizationEngine is the struct for the CEL-based authorization engine.
type AuthorizationEngine struct {
	engines []policyEngine
}

// NewAuthorizationEngine builds a CEL evaluation engine from a list of Envoy RBACs.
func NewAuthorizationEngine(rbacs []pb.RBAC) (AuthorizationEngine, error) {
	if len(rbacs) < 1 || len(rbacs) > 2 {
		return AuthorizationEngine{}, status.Errorf(codes.InvalidArgument, "must provide 1 or 2 RBACs")
	}
	if len(rbacs) == 2 && (rbacs[0].Action != pb.RBAC_DENY || rbacs[1].Action != pb.RBAC_ALLOW) {
		return AuthorizationEngine{}, status.Errorf(codes.InvalidArgument, "when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order")
	}
	var engines []policyEngine
	for _, rbac := range rbacs {
		engines = append(engines, newPolicyEngine(rbac))
	}
	return AuthorizationEngine{engines}, nil
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
func (authorizationEngine AuthorizationEngine) Evaluate(args AuthorizationArgs) (AuthorizationDecision, error) {
	evalMap := args.toEvalMap()
	numEngines := len(authorizationEngine.engines)
	if numEngines < 1 || numEngines > 2 {
		return AuthorizationDecision{}, status.Errorf(codes.Internal, "each CEL-based authorization engine should have 1 or 2 RBAC engines; instead, there are %d in the CEL-based authorization engine provided", numEngines)
	}
	// Evaluate the first engine.
	decision, policyNames := authorizationEngine.engines[0].evaluate(evalMap)
	// Evaluate the second engine, if there is one and if the first engine is unmatched.
	if len(authorizationEngine.engines) == 2 && decision == DecisionAllow {
		decision, policyNames = authorizationEngine.engines[1].evaluate(evalMap)
	}
	return AuthorizationDecision{decision, policyNames}, nil
}
