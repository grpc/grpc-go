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
	"reflect"
	"sort"
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	cel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	interpreter "github.com/google/cel-go/interpreter"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type programMock struct {
	out ref.Val
	err error
}

func (mock programMock) Eval(vars interface{}) (ref.Val, *cel.EvalDetails, error) {
	return mock.out, nil, mock.err
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
	unsuccessfulProgram = programMock{out: nil, err: status.Errorf(codes.InvalidArgument, "Unsuccessful program evaluation")}
	errProgram          = programMock{out: valMock{"missing attributes"}, err: status.Errorf(codes.InvalidArgument, "Successful program evaluation to an error result -- missing attributes")}
	trueProgram         = programMock{out: valMock{true}, err: nil}
	falseProgram        = programMock{out: valMock{false}, err: nil}

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

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestNewAuthorizationEngine(t *testing.T) {
	tests := map[string]struct {
		allow   *pb.RBAC
		deny    *pb.RBAC
		wantErr string
		errStr  string
	}{
		"too few rbacs": {
			allow:   nil,
			deny:    nil,
			wantErr: "at least one of allow, deny must be non-nil",
			errStr:  "Expected error: at least one of allow, deny must be non-nil",
		},
		"one rbac allow": {
			allow:   &pb.RBAC{Action: pb.RBAC_ALLOW},
			deny:    nil,
			wantErr: "",
			errStr:  "Expected 1 ALLOW RBAC to be successful",
		},
		"one rbac deny": {
			allow:   nil,
			deny:    &pb.RBAC{Action: pb.RBAC_DENY},
			wantErr: "",
			errStr:  "Expected 1 DENY RBAC to be successful",
		},
		"two rbacs": {
			allow:   &pb.RBAC{Action: pb.RBAC_ALLOW},
			deny:    &pb.RBAC{Action: pb.RBAC_DENY},
			wantErr: "",
			errStr:  "Expected 2 RBACs (DENY + ALLOW) to be successful",
		},
		"wrong rbac actions": {
			allow:   &pb.RBAC{Action: pb.RBAC_DENY},
			deny:    nil,
			wantErr: "allow must have action ALLOW, deny must have action DENY",
			errStr:  "Expected error: allow must have action ALLOW, deny must have action DENY",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, gotErr := NewAuthorizationEngine(tc.allow, tc.deny)
			if tc.wantErr == "" && gotErr == nil {
				return
			}
			if gotErr == nil || gotErr.Error() != tc.wantErr {
				t.Fatalf(tc.errStr)
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
			match:  false,
			want:   DecisionDeny,
		},
		"DENY engine match": {
			engine: &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{}},
			match:  true,
			want:   DecisionDeny,
		},
		"DENY engine fail": {
			engine: &policyEngine{action: pb.RBAC_DENY, programs: map[string]cel.Program{}},
			match:  false,
			want:   DecisionAllow,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getDecision(tc.engine, tc.match); got != tc.want {
				t.Fatalf("Expected %v, instead got %v", tc.want, got)
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
			if gotDecision != tc.wantDecision || !reflect.DeepEqual(gotPolicyNames, tc.wantPolicyNames) {
				t.Fatalf("Expected (%v, %v), instead got (%v, %v)", tc.wantDecision, tc.wantPolicyNames, gotDecision, gotPolicyNames)
			}
		})
	}
}

func (s) TestAuthorizationEngineEvaluate(t *testing.T) {
	tests := map[string]struct {
		engine           *AuthorizationEngine
		authArgs         *AuthorizationArgs
		wantAuthDecision *AuthorizationDecision
		wantErr          error
	}{
		"allow match": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"allow match policy2"}},
			wantErr:          nil,
		},
		"deny fail": {
			engine:           &AuthorizationEngine{deny: denyFailEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{}},
			wantErr:          nil,
		},
		"first engine unknown": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine, deny: denyUnknownEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"deny unknown policy2", "deny unknown policy3"}},
			wantErr:          nil,
		},
		"second engine match": {
			engine:           &AuthorizationEngine{allow: allowMatchEngine, deny: denyFailEngine},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"allow match policy2"}},
			wantErr:          nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gotAuthDecision, gotErr := tc.engine.Evaluate(tc.authArgs)
			sort.Strings(gotAuthDecision.policyNames)
			if tc.wantErr != nil && (gotErr == nil || gotErr.Error() != tc.wantErr.Error()) {
				t.Fatalf("Expected error to be %v, instead got %v", tc.wantErr, gotErr)
			} else if tc.wantErr == nil && (gotErr != nil || gotAuthDecision.decision != tc.wantAuthDecision.decision || !reflect.DeepEqual(gotAuthDecision.policyNames, tc.wantAuthDecision.policyNames)) {
				t.Fatalf("Expected authorization decision to be (%v, %v), instead got (%v, %v)", tc.wantAuthDecision.decision, tc.wantAuthDecision.policyNames, gotAuthDecision.decision, gotAuthDecision.policyNames)
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
			deny:             nil,
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"url_path starts with"}},
		},
		"ALLOW engine: DecisionUnknown": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path and uri_san_peer_certificate": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'", declarations)},
				"source port":                           {Condition: compileStringToExpr("source.port == 8080", declarations)},
			}},
			deny:             nil,
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:25"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"url_path and uri_san_peer_certificate"}},
		},
		"ALLOW engine: DecisionDeny": {
			allow: &pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{
				"url_path": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test'", declarations)},
			}},
			deny:             nil,
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionDeny, policyNames: []string{}},
		},
		"DENY engine: DecisionAllow": {
			allow: nil,
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"url_path and uri_san_peer_certificate": {Condition: compileStringToExpr("request.url_path == '/pkg.service/test' && connection.uri_san_peer_certificate == 'cluster/ns/default/sa/admin'", declarations)},
			}},
			authArgs:         &AuthorizationArgs{fullMethod: "/pkg.service/test/method"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{}},
		},
		"DENY engine: DecisionUnknown": {
			allow: nil,
			deny: &pb.RBAC{Action: pb.RBAC_DENY, Policies: map[string]*pb.Policy{
				"destination address": {Condition: compileStringToExpr("destination.address == '192.0.3.1'", declarations)},
				"source port":         {Condition: compileStringToExpr("source.port == 8080", declarations)},
			}},
			authArgs:         &AuthorizationArgs{peerInfo: &peer.Peer{Addr: addrMock{addr: "192.0.2.1:25"}}, fullMethod: "/pkg.service/test"},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"destination address"}},
		},
		"DENY engine: DecisionDeny": {
			allow: nil,
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
			if gotErr != nil || gotAuthDecision.decision != tc.wantAuthDecision.decision || !reflect.DeepEqual(gotAuthDecision.policyNames, tc.wantAuthDecision.policyNames) {
				t.Fatalf("Expected authorization decision to be (%v, %v), instead got (%v, %v)", tc.wantAuthDecision.decision, tc.wantAuthDecision.policyNames, gotAuthDecision.decision, gotAuthDecision.policyNames)
			}
		})
	}
}
