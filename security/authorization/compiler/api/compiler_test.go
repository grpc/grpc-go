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

package compiler

import (
	"fmt"
	"io/ioutil"
	"log"
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/protobuf/proto"
)

var testAction = "ALLOW"
var testPolicies = make(map[string]string)

func TestParseEval(t *testing.T) {
	testPolicies["test access"] = "request.url_path.startsWith('/pkg.service/test')"
	testPolicies["admin access"] = "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'"
	testPolicies["dev access"] = "request.url_path == '/pkg.service/dev' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/dev'"
	env := createUserPolicyCelEnv()
	rbac, err := CompileYamltoRbac("../user_policy.yaml")
	if err != nil {
		t.Errorf("Failed to compile %v", err)
	}
	policies := rbac.Policies
	for name, rbacPolicy := range policies {
		testPolicy := testPolicies[name]
		fmt.Println(testPolicy)
		testAst, err := compileCel(env, testPolicy)
		if err != nil {
			t.Errorf("Failed to compile %v", err)
		}
		testProgram, proErr := env.Program(testAst)
		if proErr != nil {
			t.Errorf("Failed to convert AST to Program %v", proErr)
		}
		expr := rbacPolicy.Condition
		// expr := rbacPolicy.CheckedCondition v3
		program, err := exprToProgram(env, expr)
		if err != nil {
			t.Errorf("Failed to convert expr to Program %v", err)
		}
		// program, _ := env.Program(cel.CheckedExprToAst(expr)) v3
		vars := map[string]interface{}{
			"request.url_path":                    "/pkg.service/test",
			"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin",
		}
		got, _, gotErr := (*program).Eval(vars) //(*program).Eval(vars)
		if gotErr != nil {
			t.Errorf("Error in evaluating CEL program %s", gotErr.Error())
		}
		want, _, wantErr := testProgram.Eval(vars)
		if wantErr != nil {
			t.Errorf("Error in evaluating TEST CEL program %s", wantErr.Error())
		}
		if got != want {
			t.Errorf("Error CEL prgram evaluations do not amtch up %v, %v", got, want)
		}
		fmt.Printf("Compiled rbac evaluation result: %v, Direct Evaluation result: %v \n", got, want)
	}
}

func TestSerialize(t *testing.T) {
	input := "../user_policy.yaml"
	output := "test_rbac.pb"
	err := Compile(input, output)
	if err != nil {
		log.Panicf("Failed to serialize RBAC proto %v", err)
	}
	serialRbac, readErr := ioutil.ReadFile(output)
	if readErr != nil {
		t.Errorf("Error in reading serialized RBAC proto %s", readErr.Error())
	}
	rbac := &pb.RBAC{}
	deErr := proto.Unmarshal(serialRbac, rbac)
	if deErr != nil {
		t.Errorf("Error in de-serializing RBAC proto %s", deErr.Error())
	}
	testPolicies["test access"] = "request.url_path.startsWith('/pkg.service/test')"
	testPolicies["admin access"] = "connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'"
	testPolicies["dev access"] = "request.url_path == '/pkg.service/dev' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/dev'"
	env := createUserPolicyCelEnv()
	// fmt.Println(rbac.String())
	policies := rbac.Policies
	for name, rbacPolicy := range policies {
		testPolicy := testPolicies[name]
		fmt.Println(testPolicy)
		testAst, err := compileCel(env, testPolicy)
		if err != nil {
			t.Errorf("Failed to compile %v", err)
		}
		testProgram, proErr := env.Program(testAst)
		if proErr != nil {
			t.Errorf("Failed to convert AST to Program %v", proErr)
		}
		expr := rbacPolicy.Condition
		// expr := rbacPolicy.CheckedCondition v3
		program, err := exprToProgram(env, expr)
		if err != nil {
			t.Errorf("Failed to convert expr to Program %v", err)
		}
		// program, _ := env.Program(cel.CheckedExprToAst(expr)) v3
		vars := map[string]interface{}{
			"request.url_path":                    "/pkg.service/test",
			"connection.uri_san_peer_certificate": "cluster/ns/default/sa/admin",
		}
		got, _, gotErr := (*program).Eval(vars) //(*program).Eval(vars)
		if gotErr != nil {
			t.Errorf("Error in evaluating CEL program %s", gotErr.Error())
		}
		want, _, wantErr := testProgram.Eval(vars)
		if wantErr != nil {
			t.Errorf("Error in evaluating TEST CEL program %s", wantErr.Error())
		}
		if got != want {
			t.Errorf("Error CEL prgram evaluations do not amtch up %v, %v", got, want)
		}
		fmt.Printf("Compiled rbac evaluation result: %v, Direct Evaluation result: %v \n", got, want)
	}
}
