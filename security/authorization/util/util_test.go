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

	cel "github.com/google/cel-go/cel"
	decls "github.com/google/cel-go/checker/decls"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

type policyTest struct {
	expected  bool
	expr      string
	authzArgs map[string]interface{}
}

func TestStringConvert(t *testing.T) {
	var policyTests = []policyTest{
		{
			// use a policy with startsWith primative and returns a match.
			expected:  true,
			expr:      "request.url_path.startsWith('/pkg.service/test')",
			authzArgs: map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
		{
			// use a policy with == primative and returns a match.
			expected:  true,
			expr:      "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs: map[string]interface{}{"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
		{
			// use a policy with startsWith primative and returns no match.
			expected:  false,
			expr:      "request.url_path.startsWith('/pkg.service/test')",
			authzArgs: map[string]interface{}{"request.url_path": "/source/pkg.service/test"},
		},
		{
			// use a policy with startsWith and == primatives and returns a match.
			expected: true,
			expr:     "request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'",
			authzArgs: map[string]interface{}{"request.url_path": "/pkg.service/test",
				"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin"},
		},
		{
			// use a policy that has a field not present in the environment.
			expected:  false,
			expr:      "request.source_path.startsWith('/pkg.service/test')",
			authzArgs: map[string]interface{}{"request.url_path": "/pkg.service/test"},
		},
		{
			// use an argument that has a filed not included in the environment.
			expected:  false,
			expr:      "request.url_path.startsWith('/pkg.service/test')",
			authzArgs: map[string]interface{}{"request.source_path": "/pkg.service/test"},
		},
	}
	declarations := []*expr.Decl{decls.NewIdent("request.url_path", decls.String, nil),
		decls.NewIdent("request.host", decls.String, nil),
		decls.NewIdent("request.method", decls.String, nil),
		decls.NewIdent("request.headers", decls.NewMapType(decls.String, decls.String), nil),
		decls.NewIdent("source.address", decls.String, nil),
		decls.NewIdent("source.port", decls.Int, nil),
		decls.NewIdent("source.principal", decls.String, nil),
		decls.NewIdent("destination.address", decls.String, nil),
		decls.NewIdent("destination.port", decls.Int, nil),
		decls.NewIdent("connection.uri_san_peer_certificate", decls.String, nil)}

	env, _ := cel.NewEnv(cel.Declarations(declarations...))

	for i, testInfo := range policyTests {
		expected, expr, authzArgs := testInfo.expected, testInfo.expr, testInfo.authzArgs
		checked, err := convertStringToCheckedExpr(expr, declarations)
		if err != nil {
			if i == 4 { // the 4th test should error since not in environment
				continue
			}
			t.Errorf("Error in conversion %v", err)
		}
		ast := cel.CheckedExprToAst(checked)
		program, err := env.Program(ast)
		got, _, gotErr := program.Eval(authzArgs)
		if gotErr != nil {
			if i == 5 {
				continue
			}
			t.Errorf("Error in evaluating CEL program %s", gotErr.Error())
		}
		if got.Value() != expected {
			t.Errorf("Error in evaluating converted CheckedExpr %v", got.Value())
		}
	}
}
