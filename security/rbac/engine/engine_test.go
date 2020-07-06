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
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestEmptyArgs(t *testing.T) {
	celEngine := NewCelEvaluationEngine(pb.RBAC{})
	want := DecisionUnknown
	got := celEngine.Evaluate(AuthorizationArgs{}).decision
	if got != want {
		t.Errorf("Evaluating empty RBAC policies against empty authorization arguments gives %v; want %v", got, want)
	}
}

func TestCelEngine(t *testing.T) {
	policy := pb.Policy{Condition: &expr.Expr{}}
	rbac := pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{"policy1": &policy}}

	celEngine := NewCelEvaluationEngine(rbac)
	want := DecisionAllow
	got := celEngine.Evaluate(AuthorizationArgs{}).decision
	if got != want {
		t.Errorf("Evaluating empty RBAC policies against empty authorization arguments gives %v; want %v", got, want)
	}
}
