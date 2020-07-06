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
	"fmt"
	"log"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	cel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var (
	errWrongNumberRbacs = status.Errorf(codes.InvalidArgument, "must provide 1 or 2 RBACs")
	errWrongRbacActions = status.Errorf(codes.InvalidArgument, "when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order")
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

// String returns the string representation of a Decision object
func (d Decision) String() string {
	return [...]string{"DecisionAllow", "DecisionDeny", "DecisionUnknown"}[d]
}

// AuthorizationDecision is the output of CEL engine.
type AuthorizationDecision struct {
	decision   Decision
	policyName string
}

// Converts an expression to a parsed expression, with SourceInfo nil
func exprToParsedExpr(condition *expr.Expr) *expr.ParsedExpr {
	return &expr.ParsedExpr{Expr: condition}
}

// Converts an expression to a CEL program
func exprToProgram(condition *expr.Expr) *cel.Program {
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("source", decls.String),
			decls.NewVar("origin", decls.String),
			decls.NewVar("target", decls.String),
		),
	)
	// Converts condition to ParsedExpr by setting SourceInfo empty.
	pexpr := exprToParsedExpr(condition)
	ast := cel.ParsedExprToAst(pexpr)
	prg, err := env.Program(ast)
	if err != nil {
		log.Fatalf("program construction error: %s", err)
	}
	return &prg
}

// Returns whether or not a policy is matched.
// If args is empty, the match always fails.
func matches(program *cel.Program, args AuthorizationArgs) bool {
	out, _, err := (*program).Eval(map[string]interface{}{
		// "source": args.Source(),
		// "origin": args.Origin(),
		// "target": args.Target()
	})
	if err != nil {
		log.Fatalf("evaluation error: %s", err)
	}
	fmt.Println(out) // 'true'
	return out.Value().(bool)
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

// Returns whether a set of authorization arguments match an rbacEngine's conditions.
func (engine rbacEngine) matches(args AuthorizationArgs) (bool, string) {
	for policyName, condition := range engine.conditions {
		if matches(condition, args) {
			return true, policyName
		}
	}
	return false, ""
}

// CelEvaluationEngine is the struct for CEL engine.
type CelEvaluationEngine struct {
	engines []rbacEngine
}

// NewCelEvaluationEngine builds a cel evaluation engine from a list of Envoy RBACs.
func NewCelEvaluationEngine(rbacs []pb.RBAC) (CelEvaluationEngine, error) {
	if len(rbacs) < 1 || len(rbacs) > 2 {
		return CelEvaluationEngine{}, errWrongNumberRbacs
	}
	if len(rbacs) == 2 && (rbacs[0].Action != pb.RBAC_DENY || rbacs[1].Action != pb.RBAC_ALLOW) {
		return CelEvaluationEngine{}, errWrongRbacActions
	}
	var engines []rbacEngine
	for _, rbac := range rbacs {
		engines = append(engines, newRbacEngine(rbac))
	}
	return CelEvaluationEngine{engines}, nil
}

// Evaluate is the core function that evaluates whether an RPC is authorized.
func (celEngine CelEvaluationEngine) Evaluate(args AuthorizationArgs) AuthorizationDecision {
	for _, engine := range celEngine.engines {
		match, policyName := engine.matches(args)
		if match {
			var decision Decision
			if engine.action == pb.RBAC_ALLOW {
				decision = DecisionAllow
			} else {
				decision = DecisionDeny
			}
			return AuthorizationDecision{decision, policyName}
		}
	}
	// if no RBACs matched
	var decision Decision
	if len(celEngine.engines) == 2 {
		decision = DecisionDeny
	} else if celEngine.engines[0].action == pb.RBAC_ALLOW {
		decision = DecisionDeny
	} else {
		decision = DecisionAllow
	}
	return AuthorizationDecision{decision, ""}
}
