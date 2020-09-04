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
	"net"
	"strconv"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/interpreter"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
)

var logger = grpclog.Component("authorization")

var stringAttributeMap = map[string]func(*AuthorizationArgs) (string, error){
	"request.url_path":                    (*AuthorizationArgs).getRequestURLPath,
	"request.host":                        (*AuthorizationArgs).getRequestHost,
	"request.method":                      (*AuthorizationArgs).getRequestMethod,
	"source.address":                      (*AuthorizationArgs).getSourceAddress,
	"destination.address":                 (*AuthorizationArgs).getDestinationAddress,
	"connection.uri_san_peer_certificate": (*AuthorizationArgs).getURISanPeerCertificate,
	"source.principal":                    (*AuthorizationArgs).getSourcePrincipal,
}

var intAttributeMap = map[string]func(*AuthorizationArgs) (int, error){
	"source.port":      (*AuthorizationArgs).getSourcePort,
	"destination.port": (*AuthorizationArgs).getDestinationPort,
}

// activationImpl is an implementation of interpreter.Activation.
// An Activation is the primary mechanism by which a caller supplies input into a CEL program.
type activationImpl struct {
	dict map[string]interface{}
}

// ResolveName returns a value from the activation by qualified name, or false if the name
// could not be found.
func (activation activationImpl) ResolveName(name string) (interface{}, bool) {
	result, ok := activation.dict[name]
	return result, ok
}

// Parent returns the parent of the current activation, may be nil.
// If non-nil, the parent will be searched during resolve calls.
func (activation activationImpl) Parent() interpreter.Activation {
	return activationImpl{}
}

// AuthorizationArgs is the input of the CEL-based authorization engine.
type AuthorizationArgs struct {
	md         metadata.MD
	peerInfo   *peer.Peer
	fullMethod string
}

// newActivation converts AuthorizationArgs into the activation for CEL.
func newActivation(args *AuthorizationArgs) interpreter.Activation {
	// Fill out evaluation map, only adding the attributes that can be extracted.
	evalMap := make(map[string]interface{})
	for key, function := range stringAttributeMap {
		val, err := function(args)
		if err == nil {
			evalMap[key] = val
		}
	}
	for key, function := range intAttributeMap {
		val, err := function(args)
		if err == nil {
			evalMap[key] = val
		}
	}
	val, err := args.getRequestHeaders()
	if err == nil {
		evalMap["request.headers"] = val
	}
	// Convert evaluation map to activation.
	return activationImpl{dict: evalMap}
}

func (args *AuthorizationArgs) getRequestURLPath() (string, error) {
	if args.fullMethod == "" {
		return "", fmt.Errorf("authorization args doesn't have a valid request url path")
	}
	return args.fullMethod, nil
}

func (args *AuthorizationArgs) getRequestHost() (string, error) {
	// TODO(@zhenlian): fill out attribute extraction for request.host
	return "", fmt.Errorf("authorization args doesn't have a valid request host")
}

func (args *AuthorizationArgs) getRequestMethod() (string, error) {
	// TODO(@zhenlian): fill out attribute extraction for request.method
	return "", fmt.Errorf("authorization args doesn't have a valid request method")
}

func (args *AuthorizationArgs) getRequestHeaders() (map[string]string, error) {
	// TODO(@zhenlian): fill out attribute extraction for request.headers
	return nil, fmt.Errorf("authorization args doesn't have valid request headers")
}

func (args *AuthorizationArgs) getSourceAddress() (string, error) {
	if args.peerInfo == nil {
		return "", fmt.Errorf("authorization args doesn't have a valid source address")
	}
	addr := args.peerInfo.Addr.String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	return host, nil
}

func (args *AuthorizationArgs) getSourcePort() (int, error) {
	if args.peerInfo == nil {
		return 0, fmt.Errorf("authorization args doesn't have a valid source port")
	}
	addr := args.peerInfo.Addr.String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

func (args *AuthorizationArgs) getDestinationAddress() (string, error) {
	// TODO(@zhenlian): fill out attribute extraction for destination.address
	return "", fmt.Errorf("authorization args doesn't have a valid destination address")
}

func (args *AuthorizationArgs) getDestinationPort() (int, error) {
	// TODO(@zhenlian): fill out attribute extraction for destination.port
	return 0, fmt.Errorf("authorization args doesn't have a valid destination port")
}

func (args *AuthorizationArgs) getURISanPeerCertificate() (string, error) {
	// TODO(@zhenlian): fill out attribute extraction for connection.uri_san_peer_certificate
	return "", fmt.Errorf("authorization args doesn't have a valid URI in SAN field of the peer certificate")
}

func (args *AuthorizationArgs) getSourcePrincipal() (string, error) {
	// TODO(@zhenlian): fill out attribute extraction for source.principal
	return "", fmt.Errorf("authorization args doesn't have a valid source principal")
}

// Decision represents different authorization decisions a CEL-based
// authorization engine can return.
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
func exprToProgram(condition *expr.Expr, env *cel.Env) (cel.Program, error) {
	// Converts condition to ParsedExpr by setting SourceInfo empty.
	pexpr := exprToParsedExpr(condition)
	// pretend cel.ExprToAst exists
	ast, iss := env.Check(cel.ParsedExprToAst(pexpr))
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	// Check that the expression will evaluate to a boolean.
	if !proto.Equal(ast.ResultType(), decls.Bool) {
		return nil, fmt.Errorf("expected boolean condition")
	}
	// Build the program plan.
	return env.Program(ast,
		cel.EvalOptions(cel.OptOptimize),
	)
}

// policyEngine is the struct for an engine created from one RBAC proto.
type policyEngine struct {
	action   pb.RBAC_Action
	programs map[string]cel.Program
}

// Creates a new policyEngine from an RBAC policy proto.
func newPolicyEngine(rbac *pb.RBAC, env *cel.Env) (*policyEngine, error) {
	if rbac == nil {
		return nil, nil
	}
	action := rbac.Action
	programs := make(map[string]cel.Program)
	for policyName, policy := range rbac.Policies {
		prg, err := exprToProgram(policy.Condition, env)
		if err != nil {
			return &policyEngine{}, fmt.Errorf("failed to create CEL program from condition: %v", err)
		}
		programs[policyName] = prg
	}
	return &policyEngine{action, programs}, nil
}

// Returns the decision of an engine based on whether or not AuthorizationArgs is a match,
// i.e. if engine's action is ALLOW and match is true, we will return DecisionAllow;
// if engine's action is ALLOW and match is false, we will return DecisionDeny.
func getDecision(engine *policyEngine, match bool) Decision {
	if engine.action == pb.RBAC_ALLOW && match || engine.action == pb.RBAC_DENY && !match {
		return DecisionAllow
	}
	return DecisionDeny
}

// Returns the authorization decision of a single policy engine based on activation.
// If any policy matches, the decision matches the engine's action, and the first
//  matching policy name will be returned.
// Else if any policy is missing attributes, the decision is unknown, and the list of
//  policy names that can't be evaluated due to missing attributes will be returned.
// Else, the decision is the opposite of the engine's action, i.e. an ALLOW engine
//  will return DecisionDeny, and vice versa.
func (engine *policyEngine) evaluate(activation interpreter.Activation) (Decision, []string) {
	unknownPolicyNames := []string{}
	for policyName, program := range engine.programs {
		// Evaluate program against activation.
		var match bool
		out, _, err := program.Eval(activation)
		if err != nil {
			if out == nil {
				// Unsuccessful evaluation, typically the result of a series of incompatible
				// `EnvOption` or `ProgramOption` values used in the creation of the evaluation
				// environment or executable program.
				logger.Warning("Unsuccessful evaluation encountered during AuthorizationEngine.Evaluate: %s", err.Error())
			}
			// Unsuccessful evaluation or successful evaluation to an error result, i.e. missing attributes.
			match = false
		} else {
			// Successful evaluation to a non-error result.
			if !types.IsBool(out) {
				logger.Warning("'Successful evaluation', but output isn't a boolean: %v", out)
				match = false
			} else {
				match = out.Value().(bool)
			}
		}

		// Process evaluation results.
		if err != nil {
			unknownPolicyNames = append(unknownPolicyNames, policyName)
		} else if match {
			return getDecision(engine, true), []string{policyName}
		}
	}
	if len(unknownPolicyNames) > 0 {
		return DecisionUnknown, unknownPolicyNames
	}
	return getDecision(engine, false), []string{}
}

// AuthorizationEngine is the struct for the CEL-based authorization engine.
type AuthorizationEngine struct {
	allow *policyEngine
	deny  *policyEngine
}

// NewAuthorizationEngine builds a CEL evaluation engine from at most one allow and one deny Envoy RBAC.
func NewAuthorizationEngine(allow, deny *pb.RBAC) (*AuthorizationEngine, error) {
	if allow == nil && deny == nil {
		return &AuthorizationEngine{}, fmt.Errorf("at least one of allow, deny must be non-nil")
	}
	if allow != nil && allow.Action != pb.RBAC_ALLOW || deny != nil && deny.Action != pb.RBAC_DENY {
		return nil, fmt.Errorf("allow must have action ALLOW, deny must have action DENY")
	}
	// Note: env can be shared across multiple Checks / Program constructions.
	env, err := cel.NewEnv(
		cel.Declarations(
			decls.NewVar("request.url_path", decls.String),
			decls.NewVar("request.host", decls.String),
			decls.NewVar("request.method", decls.String),
			decls.NewVar("request.headers", decls.NewMapType(decls.String, decls.String)),
			decls.NewVar("source.address", decls.String),
			decls.NewVar("source.port", decls.Int),
			decls.NewVar("destination.address", decls.String),
			decls.NewVar("destination.port", decls.Int),
			decls.NewVar("connection.uri_san_peer_certificate", decls.String),
			decls.NewVar("source.principal", decls.String),
		),
	)
	if err != nil {
		return &AuthorizationEngine{}, fmt.Errorf("failed to create CEL Env: %v", err)
	}
	// create policy engines
	allowEngine, err := newPolicyEngine(allow, env)
	if err != nil {
		return &AuthorizationEngine{}, err
	}
	denyEngine, err := newPolicyEngine(deny, env)
	if err != nil {
		return &AuthorizationEngine{}, err
	}
	return &AuthorizationEngine{allow: allowEngine, deny: denyEngine}, nil
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
func (authorizationEngine *AuthorizationEngine) Evaluate(args *AuthorizationArgs) (AuthorizationDecision, error) {
	activation := newActivation(args)
	decision := DecisionAllow
	var policyNames []string
	// Evaluate the deny engine, if it exists.
	if authorizationEngine.deny != nil {
		decision, policyNames = authorizationEngine.deny.evaluate(activation)
	}
	// Evaluate the allow engine, if it exists and if the deny engine doesn't exist or is unmatched.
	if authorizationEngine.allow != nil && decision == DecisionAllow {
		decision, policyNames = authorizationEngine.allow.evaluate(activation)
	}
	return AuthorizationDecision{decision, policyNames}, nil
}
