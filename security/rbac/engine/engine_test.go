// +build go1.10

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
	"strings"
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	cel "github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	interpreter "github.com/google/cel-go/interpreter"
	"google.golang.org/grpc/codes"
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

func TestNewAuthorizationEngine(t *testing.T) {
	tests := map[string]struct {
		input   []*pb.RBAC
		wantErr string
		errStr  string
	}{
		"too few rbacs": {
			input:   []*pb.RBAC{},
			wantErr: "code = InvalidArgument desc = must provide 1 or 2 RBACs",
			errStr:  "Expected wrong number of RBACs error for 0 RBACs",
		},
		"one rbac": {
			input:   []*pb.RBAC{{}},
			wantErr: "",
			errStr:  "",
		},
		"two rbacs": {
			input:   []*pb.RBAC{{}, {}},
			wantErr: "",
			errStr:  "",
		},
		"too many rbacs": {
			input:   []*pb.RBAC{{}, {}, {}},
			wantErr: "code = InvalidArgument desc = must provide 1 or 2 RBACs",
			errStr:  "Expected wrong number of RBACs error for 3 RBACs",
		},
		"wrong rbac actions": {
			input:   []*pb.RBAC{{Action: pb.RBAC_ALLOW}, {Action: pb.RBAC_DENY}},
			wantErr: "code = InvalidArgument desc = when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order",
			errStr:  "Expected wrong RBAC actions error for ALLOW followed by DENY",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			_, gotErr := NewAuthorizationEngine(tc.input)
			if tc.wantErr == "" && gotErr == nil {
				return
			}
			if gotErr == nil || !strings.HasSuffix(gotErr.Error(), tc.wantErr) {
				t.Errorf(tc.errStr)
			}
		})
	}
}

func TestGetDecision(t *testing.T) {
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
				t.Errorf("Expected %v, instead got %v", tc.want, got)
			}
		})
	}
}

func TestPolicyEngineEvaluate(t *testing.T) {
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
				t.Errorf("Expected (%v, %v), instead got (%v, %v)", tc.wantDecision, tc.wantPolicyNames, gotDecision, gotPolicyNames)
			}
		})
	}
}

func TestAuthorizationEngineEvaluate(t *testing.T) {
	tests := map[string]struct {
		engine           *AuthorizationEngine
		authArgs         *AuthorizationArgs
		wantAuthDecision *AuthorizationDecision
		wantErr          error
	}{
		"no engines": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{}},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{},
			wantErr:          status.Errorf(codes.Internal, "each CEL-based authorization engine should have 1 or 2 policy engines; instead, there are %d in the CEL-based authorization engine provided", 0),
		},
		"wrong engine order": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{allowMatchEngine, denyFailEngine}},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{},
			wantErr:          status.Errorf(codes.Internal, "if the CEL-based authorization engine has 2 policy engines, should be 1 DENY and 1 ALLOW in that order; instead, have %v, %v", pb.RBAC_ALLOW, pb.RBAC_DENY),
		},
		"allow match": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{allowMatchEngine}},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{"allow match policy2"}},
			wantErr:          nil,
		},
		"deny fail": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{denyFailEngine}},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionAllow, policyNames: []string{}},
			wantErr:          nil,
		},
		"first engine unknown": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{denyUnknownEngine, allowMatchEngine}},
			authArgs:         &AuthorizationArgs{},
			wantAuthDecision: &AuthorizationDecision{decision: DecisionUnknown, policyNames: []string{"deny unknown policy2", "deny unknown policy3"}},
			wantErr:          nil,
		},
		"second engine match": {
			engine:           &AuthorizationEngine{engines: []*policyEngine{denyFailEngine, allowMatchEngine}},
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
				t.Errorf("Expected error to be %v, instead got %v", tc.wantErr, gotErr)
			} else if tc.wantErr == nil && (gotErr != nil || gotAuthDecision.decision != tc.wantAuthDecision.decision || !reflect.DeepEqual(gotAuthDecision.policyNames, tc.wantAuthDecision.policyNames)) {
				t.Errorf("Expected authorization decision to be (%v, %v), instead got (%v, %v)", tc.wantAuthDecision.decision, tc.wantAuthDecision.policyNames, gotAuthDecision.decision, gotAuthDecision.policyNames)
			}
		})
	}
}
