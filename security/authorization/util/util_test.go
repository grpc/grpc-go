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

package engine

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"

	cel "github.com/google/cel-go/cel"
	decls "github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func TestStringConvert(t *testing.T) {
	declarations := []*expr.Decl{
		decls.NewIdent("request.url_path", decls.String, nil),
		decls.NewIdent("request.host", decls.String, nil),
		decls.NewIdent("request.method", decls.String, nil),
		decls.NewIdent("request.headers", decls.NewMapType(decls.String, decls.String), nil),
		decls.NewIdent("source.address", decls.String, nil),
		decls.NewIdent("source.port", decls.Int, nil),
		decls.NewIdent("source.principal", decls.String, nil),
		decls.NewIdent("destination.address", decls.String, nil),
		decls.NewIdent("destination.port", decls.Int, nil),
		decls.NewIdent("connection.uri_san_peer_certificate", decls.String, nil)}
	env, _ := cel.NewEnv() // External Evaluation env does not have to be built the same
	for _, test := range []struct {
		desc                      string
		expectedEvaluationOutcome bool
		expectedParsingError      bool
		expectedEvaluationError   bool
		expr                      string
		authzArgs                 map[string]interface{}
	}{
		{
			desc:                      "1: use a policy with startsWith primative and returns a match.",
			expectedEvaluationOutcome: true,
			expr:                      "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:                 map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
		{
			desc:                      "2: use a policy with == primative and returns a match.",
			expectedEvaluationOutcome: true,
			expr:                      "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs:                 map[string]interface{}{"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
		{
			desc:                      "3: use a policy with startsWith primative and returns no match.",
			expectedEvaluationOutcome: false,
			expr:                      "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:                 map[string]interface{}{"request.url_path": "/source/pkg.service/test"},
		},
		{
			desc:                      "4: use a policy with startsWith and == primatives and returns a match.",
			expectedEvaluationOutcome: true,
			expr:                      "request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs: map[string]interface{}{"request.url_path": "/pkg.service/test",
				"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
		{
			desc:                 "5: use a policy that has a field not present in the environment.",
			expectedParsingError: true,
			expr:                 "request.source_path.startsWith('/pkg.service/test')",
			authzArgs:            map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
		{
			desc:                    "6: use an argument that has a filed not included in the environment.",
			expectedEvaluationError: true,
			expr:                    "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:               map[string]interface{}{"request.source_path": "/pkg.service/test"},
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			expected, expr, authzArgs := test.expectedEvaluationOutcome, test.expr, test.authzArgs
			checked, err := convertStringToCheckedExpr(expr, declarations)
			if test.expectedParsingError {
				if err == nil {
					t.Errorf("lack of error in conversion %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("error in conversion %v", err)
			}
			ast := cel.CheckedExprToAst(checked)
			program, err := env.Program(ast)
			if err != nil {
				t.Errorf("error in formatting Program %v", err)
			}
			got, _, gotErr := program.Eval(authzArgs)
			if test.expectedEvaluationError {
				if gotErr == nil {
					t.Errorf("lack of error in evaluation %v", err)
				}
				return
			}
			if gotErr != nil {
				t.Errorf("error in evaluating CEL program %s", gotErr.Error())
			}
			if got.Value() != expected {
				t.Errorf("error in evaluating converted CheckedExpr %v", got.Value())
			}
		})
	}
}
