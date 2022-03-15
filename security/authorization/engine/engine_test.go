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
	"context"
	"reflect"
	"sort"
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
	"github.com/google/go-cmp/cmp"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type s struct {
	grpctest.Tester
}

type fakeProgram struct {
	out ref.Val
	err error
}

func (fake fakeProgram) Eval(vars interface{}) (ref.Val, *cel.EvalDetails, error) {
	return fake.out, nil, fake.err
}

func (fake fakeProgram) ContextEval(ctx context.Context, vars interface{}) (ref.Val, *cel.EvalDetails, error) {
	return fake.Eval(vars)
}

type valMock struct {
	val interface{}
}

func (mock valMock) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	return nil, nil
}

func (mock valMock) ConvertToType(typeValue ref.Type) ref.Val {
	return nil
}

func (mock valMock) Equal(other ref.Val) ref.Val {
	return nil
}

func (mock valMock) Type() ref.Type {
	if mock.val == true || mock.val == false {
		return types.BoolType
	}
	return nil
}

func (mock valMock) Value() interface{} {
	return mock.val
}

type addrMock struct {
	addr string
}

func (mock addrMock) Network() string {
	return "tcp"
}

func (mock addrMock) String() string {
	return mock.addr
}

var (
	emptyActivation     = interpreter.EmptyActivation()
	unsuccessfulProgram = fakeProgram{out: nil, err: status.Errorf(codes.InvalidArgument, "Unsuccessful program evaluation")}
	errProgram          = fakeProgram{out: valMock{"missing attributes"}, err: status.Errorf(codes.InvalidArgument, "Successful program evaluation to an error result -- missing attributes")}
	trueProgram         = fakeProgram{out: valMock{true}, err: nil}
	falseProgram        = fakeProgram{out: valMock{false}, err: nil}

	allowMatchEngine = &policyEngine{action: pb.RBAC_ALLOW, programs: map[string]cel.Program{
		"allow match policy1": unsuccessfulProgram,
		"allow match policy2": trueProgram,
		"allow match policy3": falseProgram,
		"allow match policy4": errProgram,
	}}
	denyFailEngine = &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{
		"deny fail policy1": falseProgram,
		"deny fail policy2": falseProgram,
		"deny fail policy3": falseProgram,
	}}
	denyUnknownEngine = &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{
		"deny unknown policy1": falseProgram,
		"deny unknown policy2": unsuccessfulProgram,
		"deny unknown policy3": errProgram,
		"deny unknown policy4": falseProgram,
	}}
)

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewAuthorizationEngine(t *testing.T) {
	tests := map[string]struct {
		allow   *pb.RBAC
		deny    *pb.RBAC
		wantErr bool
		errStr  string
	}{
		"too few rbacs": {
			wantErr: true,
			errStr:  "Expected error: at least one of allow, deny must be non-nil",
		},
		"one rbac allow": {
			allow:  &pb.RBAC{Action: pb.RBAC_ALLOW},
			errStr: "Expected 1 ALLOW RBAC to be successful",
		},
		"one rbac deny": {
			deny:   &pb.RBAC{Action: pb.RBAC_DENY},
			errStr: "Expected 1 DENY RBAC to be successful",
		},
		"two rbacs": {
			allow:  &pb.RBAC{Action: pb.RBAC_ALLOW},
			deny:   &pb.RBAC{Action: pb.RBAC_DENY},
			errStr: "Expected 2 RBACs (DENY + ALLOW) to be successful",
		},
		"wrong rbac actions": {
			allow:   &pb.RBAC{Action: pb.RBAC_DENY},
			wantErr: true,
			errStr:  "Expected error: allow must have action ALLOW, deny must have action DENY",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, gotErr := NewAuthorizationEngine(tc.allow, tc.deny)
			if (gotErr != nil) != tc.wantErr {
				t.Fatal(tc.errStr)
			}
		})
	}
}

func (s) TestGetDecision(t *testing.T) {
	tests := map[string]struct {
		engine *policyEngine
		match  bool
		want   Decision
	}{
		"ALLOW engine match": {
			engine: &policyEngine{action: pb.RBAC_ALLOW, programs: map[string]cel.Program{}},
			match:  true,
			want:   DecisionAllow,
		},
		"ALLOW engine fail": {
			engine: &policyEngine{action: pb.RBAC_ALLOW, programs: map[string]cel.Program{}},
			want:   DecisionDeny,
		},
		"DENY engine match": {
			engine: &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{}},
			match:  true,
			want:   DecisionDeny,
		},
		"DENY engine fail": {
			engine: &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{}},
			want:   DecisionAllow,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getDecision(tc.engine, tc.match); got != tc.want {
				t.Fatalf("getDecision(%v, %v) = (%v), want (%v)", tc.engine, tc.match, got, tc.want)
			}
		})
	}
}

func (s) TestPolicyEngineEvaluate(t *testing.T) {
	tests := map[string]struct {
		engine          *policyEngine
		activation      interpreter.Activation
		wantDecision    Decision
		wantPolicyNames []string
	}{
		"no policies": {
			engine:          &policyEngine{},
			activation:      emptyActivation,
			wantDecision:    DecisionDeny,
			wantPolicyNames: []string{},
		},
		"match succeed": {
			engine:          allowMatchEngine,
			activation:      emptyActivation,
			wantDecision:    DecisionAllow,
			wantPolicyNames: []string{"allow match policy2"},
		},
		"match fail": {
			engine:          denyFailEngine,
			activation:      emptyActivation,
			wantDecision:    DecisionAllow,
			wantPolicyNames: []string{},
		},
		"unknown": {
			engine:          denyUnknownEngine,
			activation:      emptyActivation,
			wantDecision:    DecisionUnknown,
			wantPolicyNames: []string{"deny unknown policy2", "deny unknown policy3"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotDecision, gotPolicyNames := tc.engine.evaluate(tc.activation)
			sort.Strings(gotPolicyNames)
			if gotDecision != tc.wantDecision || !cmp.Equal(gotPolicyNames, tc.wantPolicyNames) {
				t.Fatalf("policyEngine.evaluate(%v, %v) = (%v, %v), want (%v, %v)", tc.engine, tc.activation, gotDecision, gotPolicyNames, tc.wantDecision, tc.wantPolicyNames)
			}
		})
	}
}

func (s) TestAuthorizationEngineEvaluate(t *testing.T) {
	tests := map[string]struct {
		engine           *AuthorizationEngine
		authArgs         *AuthorizationArgs
		wantAuthDecision *AuthorizationDecision
	}{
		"allow match": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"allow match policy2"}},
		},
		"deny fail": {
			engine:           &AuthorizationEngine{deny: denyFailEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{}},
		},
		"first engine unknown": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine, deny: denyUnknownEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"deny unknown policy2", "deny unknown policy3"}},
		},
		"second engine match": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine, deny: denyFailEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"allow match policy2"}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotAuthDecision, gotErr := tc.engine.Evaluate(tc.authArgs)
			sort.Strings(gotAuthDecision.policyNames)
			if gotErr != nil || gotAuthDecision.decision != tc.wantAuthDecision.decision || !cmp.Equal(gotAuthDecision.policyNames, tc.wantAuthDecision.policyNames) {
				t.Fatalf("AuthorizationEngine.Evaluate(%v, %v) = (%v, %v), want (%v, %v)", tc.engine, tc.authArgs, gotAuthDecision, gotErr, tc.wantAuthDecision, nil)
			}
		})
	}
}

func (s) TestIntegration(t *testing.T) {
	declarations := []*expr.Decl{
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
	}

	tests := map[string]struct {
		allow            *pb.RBAC
		deny             *pb.RBAC
		authArgs         *AuthorizationArgs
		wantAuthDecision *AuthorizationDecision
	}{
		"ALLOW engine: DecisionAllow": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path starts with": {Condition: compileStringToExpr("request.url_path.startsWith('/pkg.service/test')", declarations)},
			}},
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"url_path starts with"}},
		},
		"ALLOW engine: DecisionUnknown": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path and uri_san_peer_certificate": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'", declarations)},
				"source port":                           {Condition: compileStringToExpr("source.port == 8080", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:25"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"url_path and uri_san_peer_certificate"}},
		},
		"ALLOW engine: DecisionDeny": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionDeny, policyNames: []string{}},
		},
		"DENY engine: DecisionAllow": {
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"url_path and uri_san_peer_certificate": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{}},
		},
		"DENY engine: DecisionUnknown": {
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"destination address": {Condition: compileStringToExpr("destination.address == '192.0.3.1'", declarations)},
				"source port":         {Condition: compileStringToExpr("source.port == 8080", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:25"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"destination address"}},
		},
		"DENY engine: DecisionDeny": {
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"destination address":           {Condition: compileStringToExpr("destination.address == '192.0.3.1'", declarations)},
				"source address or source port": {Condition: compileStringToExpr("source.address == '192.0.4.1' || source.port == 8080", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionDeny, policyNames: []string{"source address or source port"}},
		},
		"DENY ALLOW engine: DecisionDeny from DENY policy": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path starts with": {Condition: compileStringToExpr("request.url_path.startsWith('/pkg.service/test')", declarations)},
			}},
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"destination address":           {Condition: compileStringToExpr("destination.address == '192.0.3.1'", declarations)},
				"source address or source port": {Condition: compileStringToExpr("source.address == '192.0.4.1' || source.port == 8080", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionDeny, policyNames: []string{"source address or source port"}},
		},
		"DENY ALLOW engine: DecisionUnknown from DENY policy": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path starts with": {Condition: compileStringToExpr("request.url_path.startsWith('/pkg.service/test')", declarations)},
			}},
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"destination address":              {Condition: compileStringToExpr("destination.address == '192.0.3.1'", declarations)},
				"source port and destination port": {Condition: compileStringToExpr("source.port == 8080 && destination.port == 1234", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"destination address", "source port and destination port"}},
		},
		"DENY ALLOW engine: DecisionAllow from ALLOW policy": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"method or url_path starts with": {Condition: compileStringToExpr("request.method == 'POST' || request.url_path.startsWith('/pkg.service/test')", declarations)},
			}},
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"source address":           {Condition: compileStringToExpr("source.address == '192.0.3.1'", declarations)},
				"source port and url_path": {Condition: compileStringToExpr("source.port == 8080 && request.url_path == 'pkg.service/test'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"method or url_path starts with"}},
		},
		"DENY ALLOW engine: DecisionUnknown from ALLOW policy": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path starts with and method": {Condition: compileStringToExpr("request.url_path.startsWith('/pkg.service/test') && request.method == 'POST'", declarations)},
			}},
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"source address":           {Condition: compileStringToExpr("source.address == '192.0.3.1'", declarations)},
				"source port and url_path": {Condition: compileStringToExpr("source.port == 8080 && request.url_path == 'pkg.service/test'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"url_path starts with and method"}},
		},
		"DENY ALLOW engine: DecisionDeny from ALLOW policy": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path starts with and source port": {Condition: compileStringToExpr("request.url_path.startsWith('/pkg.service/test') && source.port == 1234", declarations)},
			}},
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"source address":           {Condition: compileStringToExpr("source.address == '192.0.3.1'", declarations)},
				"source port and url_path": {Condition: compileStringToExpr("source.port == 8080 && request.url_path == 'pkg.service/test'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:8080"}}, fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionDeny, policyNames: []string{}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			engine, err := NewAuthorizationEngine(tc.allow, tc.deny)
			if err != nil {
				t.Fatalf("Error constructing authorization engine: %v", err)
			}
			gotAuthDecision, gotErr := engine.Evaluate(tc.authArgs)
			sort.Strings(gotAuthDecision.policyNames)
			if gotErr != nil || gotAuthDecision.decision != tc.wantAuthDecision.decision || !cmp.Equal(gotAuthDecision.policyNames, tc.wantAuthDecision.policyNames) {
				t.Fatalf("NewAuthorizationEngine(%v, %v).Evaluate(%v) = (%v, %v), want (%v, %v)", tc.allow, tc.deny, tc.authArgs, gotAuthDecision, gotErr, tc.wantAuthDecision, nil)
			}
		})
	}
}
