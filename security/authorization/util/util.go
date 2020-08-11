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

// Package util is a utility package
package util

import (

	// v3
	cel "github.com/google/cel-go/cel"
	decls "github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/proto"
)

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
