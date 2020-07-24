package compiler

import (
	"fmt"
	"io/ioutil"
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
	rbac := CompileYamltoRbac("../user_policy.yaml")
	// fmt.Println(rbac.String())
	policies := rbac.Policies

	for name, rbacPolicy := range policies {
		testPolicy := testPolicies[name]
		fmt.Println(testPolicy)
		testAst := compileCel(env, testPolicy)
		testProgram, _ := env.Program(testAst)

		expr := rbacPolicy.Condition
		// expr := rbacPolicy.CheckedCondition v3
		program := exprToProgram(env, expr)
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

		// get evaluatable program from rbacPolicy and thedn evaluate both
		// need to create the test IMPORT NEEDED THINGS TO CREATE THE REQUEST OBJECT
	}

}

func TestSerialize(t *testing.T) {
	input := "../user_policy.yaml"
	output := "test_rbac"

	Compile(input, output)

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
		testAst := compileCel(env, testPolicy)
		testProgram, _ := env.Program(testAst)

		expr := rbacPolicy.Condition
		// expr := rbacPolicy.CheckedCondition v3
		program := exprToProgram(env, expr)
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

		// get evaluatable program from rbacPolicy and thedn evaluate both
		// need to create the test IMPORT NEEDED THINGS TO CREATE THE REQUEST OBJECT
	}

}
