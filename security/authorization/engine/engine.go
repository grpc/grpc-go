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
	"log"
	"net"
	"strconv"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	cel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	types "github.com/google/cel-go/common/types"
	interpreter "github.com/google/cel-go/interpreter"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var (
	errNoRequestHost           = status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid request host")
	errNoRequestMethod         = status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid request method")
	errNoRequestHeaders        = status.Errorf(codes.InvalidArgument, "authorization args doesn't have valid request headers")
	errNoDestinationAddress    = status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid destination address")
	errNoDestinationPort       = status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid destination port")
	errNoURISanPeerCertificate = status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid URI in SAN field of the peer certificate")
)

// AuthorizationArgs is the input of the CEL-based authorization engine.
type AuthorizationArgs struct {
	md         metadata.MD
	peerInfo   *peer.Peer
	fullMethod string
}

// ActivationImpl is an implementation of interpreter.Activation.
// An Activation is the primary mechanism by which a caller supplies input into a CEL program.
type ActivationImpl struct {
	dict map[string]interface{}
}

// ResolveName returns a value from the activation by qualified name, or false if the name
// could not be found.
func (activation ActivationImpl) ResolveName(name string) (interface{}, bool) {
	result, ok := activation.dict[name]
	return result, ok
}

// Parent returns the parent of the current activation, may be nil.
// If non-nil, the parent will be searched during resolve calls.
func (activation ActivationImpl) Parent() interpreter.Activation {
	return ActivationImpl{}
}

var stringAttributeMap = map[string]func(AuthorizationArgs) (string, error){
	"request.url_path":                    AuthorizationArgs.getRequestURLPath,
	"request.host":                        AuthorizationArgs.getRequestHost,
	"request.method":                      AuthorizationArgs.getRequestMethod,
	"source.address":                      AuthorizationArgs.getSourceAddress,
	"destination.address":                 AuthorizationArgs.getDestinationAddress,
	"connection.uri_san_peer_certificate": AuthorizationArgs.getURISanPeerCertificate,
}

var intAttributeMap = map[string]func(AuthorizationArgs) (int, error){
	"source.port":      AuthorizationArgs.getSourcePort,
	"destination.port": AuthorizationArgs.getDestinationPort,
}

// Converts AuthorizationArgs into the activation for CEL.
func (args AuthorizationArgs) toActivation() interpreter.Activation {
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
	return ActivationImpl{dict: evalMap}
}

func (args AuthorizationArgs) getRequestURLPath() (string, error) {
	if args.fullMethod == "" {
		return "", status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid request url path")
	}
	return args.fullMethod, nil
}

func (args AuthorizationArgs) getRequestHost() (string, error) {
	return "", errNoRequestHost
}

func (args AuthorizationArgs) getRequestMethod() (string, error) {
	return "", errNoRequestMethod
}

func (args AuthorizationArgs) getRequestHeaders() (map[string]string, error) {
	// get this from metadata?
	return nil, errNoRequestHeaders
}

func (args AuthorizationArgs) getSourceAddress() (string, error) {
	if args.peerInfo == nil {
		return "", status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid source address")
	}
	addr := args.peerInfo.Addr.String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	return host, nil
}

func (args AuthorizationArgs) getSourcePort() (int, error) {
	if args.peerInfo == nil {
		return 0, status.Errorf(codes.InvalidArgument, "authorization args doesn't have a valid source port")
	}
	addr := args.peerInfo.Addr.String()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(port)
}

func (args AuthorizationArgs) getDestinationAddress() (string, error) {
	return "", errNoDestinationAddress
}

func (args AuthorizationArgs) getDestinationPort() (int, error) {
	return 0, errNoDestinationPort
}

func (args AuthorizationArgs) getURISanPeerCertificate() (string, error) {
	return "", errNoURISanPeerCertificate
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
func exprToProgram(condition *expr.Expr) cel.Program {
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
		),
	)
	// Converts condition to ParsedExpr by setting SourceInfo empty.
	pexpr := exprToParsedExpr(condition)
	ast := cel.ParsedExprToAst(pexpr)
	prg, err := env.Program(ast)
	if err != nil {
		log.Fatalf("program construction error: %s", err)
	}
	return prg
}

// policyEngine is the struct for an engine created from one RBAC proto.
type policyEngine struct {
	action   pb.RBAC_Action
	programs map[string]cel.Program
}

// Creates a new policyEngine from an RBAC policy proto.
func newPolicyEngine(rbac *pb.RBAC) *policyEngine {
	action := rbac.Action
	programs := make(map[string]cel.Program)
	for policyName, policy := range rbac.Policies {
		programs[policyName] = exprToProgram(policy.Condition)
	}
	return &policyEngine{action, programs}
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
				grpclog.Warning("Unsuccessful evaluation encountered during AuthorizationEngine.Evaluate: %s", err.Error())
			}
			// Unsuccessful evaluation or successful evaluation to an error result, i.e. missing attributes.
			match = false
		} else {
			// Successful evaluation to a non-error result.
			if !types.IsBool(out) {
				grpclog.Warning("'Successful evaluation', but output isn't a boolean: %v", out)
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
	engines []*policyEngine
}

// NewAuthorizationEngine builds a CEL evaluation engine from a list of Envoy RBACs.
func NewAuthorizationEngine(rbacs []*pb.RBAC) (*AuthorizationEngine, error) {
	if len(rbacs) < 1 || len(rbacs) > 2 {
		return &AuthorizationEngine{}, status.Errorf(codes.InvalidArgument, "must provide 1 or 2 RBACs")
	}
	if len(rbacs) == 2 && (rbacs[0].Action != pb.RBAC_DENY || rbacs[1].Action != pb.RBAC_ALLOW) {
		return &AuthorizationEngine{}, status.Errorf(codes.InvalidArgument, "when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order")
	}
	var engines []*policyEngine
	for _, rbac := range rbacs {
		engines = append(engines, newPolicyEngine(rbac))
	}
	return &AuthorizationEngine{engines}, nil
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
	activation := args.toActivation()
	numEngines := len(authorizationEngine.engines)
	// If CelEvaluationEngine is used correctly, the below 2 errors should theoretically never occur.
	// However, to ease potential debugging, they are checked anyway.
	if numEngines < 1 || numEngines > 2 {
		return AuthorizationDecision{}, status.Errorf(codes.Internal, "each CEL-based authorization engine should have 1 or 2 policy engines; instead, there are %d in the CEL-based authorization engine provided", numEngines)
	}
	if len(authorizationEngine.engines) == 2 && (authorizationEngine.engines[0].action != pb.RBAC_DENY || authorizationEngine.engines[1].action != pb.RBAC_ALLOW) {
		return AuthorizationDecision{}, status.Errorf(codes.Internal, "if the CEL-based authorization engine has 2 policy engines, should be 1 DENY and 1 ALLOW in that order; instead, have %v, %v", authorizationEngine.engines[0].action, authorizationEngine.engines[1].action)
	}
	// Evaluate the first engine.
	decision, policyNames := authorizationEngine.engines[0].evaluate(activation)
	// Evaluate the second engine, if there is one and if the first engine is unmatched.
	if len(authorizationEngine.engines) == 2 && decision == DecisionAllow {
		decision, policyNames = authorizationEngine.engines[1].evaluate(activation)
	}
	return AuthorizationDecision{decision, policyNames}, nil
}
