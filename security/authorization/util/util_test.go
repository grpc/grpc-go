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

var declarations = []*expr.Decl{
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
var env, _ = cel.NewEnv() // External Evaluation env does not have to be built the same.

func (s) TestStringConvert(t *testing.T) {
	for _, test := range []struct {
		desc             string
		wantEvalOutcome  bool
		wantParsingError bool
		wantEvalError    bool
		expr             string
		authzArgs        map[string]interface{}
	}{
		{
			desc:            "Test 1 single primative match",
			wantEvalOutcome: true,
			expr:            "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:       map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
		{
			desc:            "Test 2 single compare match",
			wantEvalOutcome: true,
			expr:            "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs:       map[string]interface{}{"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
		{
			desc:            "Test 3 single primative no match",
			wantEvalOutcome: false,
			expr:            "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:       map[string]interface{}{"request.url_path": "/source/pkg.service/test"},
		},
		{
			desc:            "Test 4 primative and compare match",
			wantEvalOutcome: true,
			expr:            "request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs: map[string]interface{}{"request.url_path": "/pkg.service/test",
				"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			expected, expr, authzArgs := test.wantEvalOutcome, test.expr, test.authzArgs
			checked, err := convertStringToCheckedExpr(expr, declarations)
			want, got := test.wantParsingError, err != nil
			if got != want {
				t.Fatalf("Error in conversion, test.wantParsingError =%v, got %v", want, got)
			}
			ast := cel.CheckedExprToAst(checked)
			program, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Error in formatting Program %v", err)
			}
			recieved, _, gotErr := program.Eval(authzArgs)
			want, got = test.wantEvalError, gotErr != nil
			if got != want {
				t.Fatalf("Error in evaluation, test.wantEvalError =%v, got %v", want, got)
			}
			if recieved.Value() != expected {
				t.Fatalf("Error in evaluating converted CheckedExpr %v", recieved.Value())
			}
		})
	}
}

func (s) TestFailStringParse(t *testing.T) {
	for _, test := range []struct {
		desc             string
		wantParsingError bool
		expr             string
		authzArgs        map[string]interface{}
	}{
		{
			desc:             "Test 5 parse error field not present in environment",
			wantParsingError: true,
			expr:             "request.source_path.startsWith('/pkg.service/test')",
			authzArgs:        map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			expr := test.expr
			_, err := convertStringToCheckedExpr(expr, declarations)
			want, got := test.wantParsingError, err != nil
			if got && want { // early exit for valid error
				return
			}
			t.Fatalf("Error in conversion, test.wantParsingError =%v, got %v", want, got)
		})
	}
}

func (s) TestFailStringEval(t *testing.T) {
	for _, test := range []struct {
		desc          string
		wantEvalError bool
		expr          string
		authzArgs     map[string]interface{}
	}{
		{
			desc:          "Test 6 eval error argument not included in environment",
			wantEvalError: true,
			expr:          "request.url_path.startsWith('/pkg.service/test')",
			authzArgs:     map[string]interface{}{"request.source_path": "/pkg.service/test"},
		},
	} {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			expr, authzArgs := test.expr, test.authzArgs
			checked, err := convertStringToCheckedExpr(expr, declarations)
			if err != nil {
				t.Fatalf("Error in conversion got %v", err)
			}
			ast := cel.CheckedExprToAst(checked)
			program, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Error in formatting Program %v", err)
			}
			_, _, gotErr := program.Eval(authzArgs)
			want, got := test.wantEvalError, gotErr != nil
			if got && want { // early exit for valid error
				return
			}
			t.Fatalf("Error in evaluation, test.wantEvalError =%v, got %v", want, got)
		})
	}
}
