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

package util

import (
	"fmt"
	"testing"

	cel "github.com/google/cel-go/cel"
	decls "github.com/google/cel-go/checker/decls"
)

var testAction = "ALLOW"
var testPolicies = make(map[string]string)

func TestStringConvert(t *testing.T) {
	testPolicies["test access"] = "request.url_path.startsWith('/pkg.service/test')"
	testPolicies["admin access"] = "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'"
	testPolicies["dev access"] = "request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'"
	vars := map[string]interface{}{
		"request.url_path":                    "/pkg.service/test",
		"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin",
	}
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

	for _, expr := range testPolicies {
		checked, err := convertStringToCheckedExpr(expr)
		if err != nil {
			t.Errorf("Error in conversion %v", err)
		}
		ast := cel.CheckedExprToAst(checked)
		program, err := env.Program(ast)
		got, _, gotErr := program.Eval(vars) //(*program).Eval(vars)
		if gotErr != nil {
			t.Errorf("Error in evaluating CEL program %s", gotErr.Error())
		}
		if got.Value() == false {
			t.Errorf("Error in evaluating converted CheckedExpr")
		}
	}
	fmt.Println("Conversion Success")
}
