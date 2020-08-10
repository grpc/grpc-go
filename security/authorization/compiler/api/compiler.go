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
 *
 */

// Package compiler is a utility library containing functions to compile
// a User policy yaml into a serialized RBAC proto object.
package compiler

import (
	"io/ioutil"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2" // v3
	cel "github.com/google/cel-go/cel"
	decls "github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v2"
)

// UserPolicy is the user policy.
type UserPolicy struct {
	Action string `yaml:"action"`
	Rules  []struct {
		Name string `yaml:"name"`
		Expr string `yaml:"expr"`
	} `yaml:"rules"`
}

// ReadYaml reads in yaml file.
func ReadYaml(filePath string) ([]byte, error) {
	return ioutil.ReadFile(filePath)
}

func parseYaml(file []byte, policy *UserPolicy) error {
	return yaml.Unmarshal(file, policy)
}

func createUserPolicyCelEnv() *cel.Env {
	env, _ := cel.NewEnv(
		cel.Declarations(
			decls.NewIdent("request.url_path", decls.String, nil),
			decls.NewIdent("request.host", decls.String, nil),
			decls.NewIdent("request.method", decls.String, nil),
			decls.NewIdent("request.headers", decls.NewMapType(decls.String, decls.String), nil),
			decls.NewIdent("source.address", decls.String, nil),
			decls.NewIdent("source.port", decls.Int, nil),
			decls.NewIdent("source.principal", decls.String, nil),
			decls.NewIdent("destination.address", decls.String, nil),
			decls.NewIdent("destination.port", decls.Int, nil),
			decls.NewIdent("connection.uri_san_peer_certificate", decls.String, nil)))
	return env
}

// CompileYamltoRbac compiles yaml to rbac.
func CompileYamltoRbac(filename string) (*pb.RBAC, error) {
	yamlFile, err := ReadYaml(filename)
	if err != nil {
		return nil, err
	}
	var userPolicy UserPolicy
	err = parseYaml(yamlFile, &userPolicy)
	if err != nil {
		return nil, err
	}
	env := createUserPolicyCelEnv()
	var rbac pb.RBAC
	rbac.Action = pb.RBAC_Action(pb.RBAC_Action_value[userPolicy.Action])
	rbac.Policies = make(map[string]*pb.Policy)
	for index := range userPolicy.Rules {
		rule := userPolicy.Rules[index]
		name := rule.Name
		expr := rule.Expr
		var policy pb.Policy
		checked, err := compileCel(env, expr)
		if err != nil {
			return nil, err
		}
		// checkedExpr, err := cel.AstToCheckedExpr(checked) // v3
		// if err != nil {
		// 	log.Panicf("Failed Converting AST to Checked Express %v", err)
		// }
		// policy.CheckedCondition = checkedExpr    // v3
		checkedExpr := checked.Expr()  // v2
		policy.Condition = checkedExpr // v2
		rbac.Policies[name] = &policy
	}
	return &rbac, nil
}

// Converts an expression to a parsed expression, with SourceInfo nil.
func exprToParsedExpr(expression *expr.Expr) *expr.ParsedExpr {
	return &expr.ParsedExpr{Expr: expression}
}

// Converts an expression to a CEL program.
func exprToProgram(env *cel.Env, expr *expr.Expr) (*cel.Program, error) {
	// ONLY NEEDED FOR V2
	// v3: can replace line with ast := cel.CheckedExprToAst(checkedExpr)
	ast := cel.ParsedExprToAst(exprToParsedExpr(expr))
	program, err := env.Program(ast)
	return &program, err
}

// Compile takes in input file name and returns serialized output rbac proto.
func Compile(inputFilename string, outputFilename string) error {
	rbac, rErr := CompileYamltoRbac(inputFilename)
	if rErr != nil {
		return rErr
	}
	serialized, err := proto.Marshal(rbac)
	ioutil.WriteFile(outputFilename, serialized, 0644)
	return err
}

func compileCel(env *cel.Env, expr string) (*cel.Ast, error) {
	ast, iss := env.Parse(expr)
	// Report syntactic errors, if present.
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	// Type-check the expression for correctness.
	checked, iss := env.Check(ast)
	if iss.Err() != nil {
		return nil, iss.Err()
	}
	// Check the result type is a Boolean.
	if !proto.Equal(checked.ResultType(), decls.Bool) {
		return nil, iss.Err()
	}
	return checked, nil
}

func convertStringToCheckedExpr(expr string) (*expr.CheckedExpr, error) {
	env, _ := cel.NewEnv(
		cel.Declarations(
			decls.NewIdent("request.url_path", decls.String, nil),
			decls.NewIdent("request.host", decls.String, nil),
			decls.NewIdent("request.method", decls.String, nil),
			decls.NewIdent("request.headers", decls.NewMapType(decls.String, decls.String), nil),
			decls.NewIdent("source.address", decls.String, nil),
			decls.NewIdent("source.port", decls.Int, nil),
			decls.NewIdent("source.principal", decls.String, nil),
			decls.NewIdent("destination.address", decls.String, nil),
			decls.NewIdent("destination.port", decls.Int, nil),
			decls.NewIdent("connection.uri_san_peer_certificate", decls.String, nil)))

	checked, err := compileCel(env, expr)
	if err != nil {
		return nil, err
	}
	checkedExpr, err := cel.AstToCheckedExpr(checked)
	if err != nil {
		return nil, err
	}

	return checkedExpr, nil

}
