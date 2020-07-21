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
	"strings"
	"testing"

	pb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	// expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

func TestTooFewRbacs(t *testing.T) {
	_, got := NewAuthorizationEngine([]pb.RBAC{})
	if got == nil || !strings.HasSuffix(got.Error(), "code = InvalidArgument desc = must provide 1 or 2 RBACs") {
		t.Errorf("Expected wrong number of RBACs error for 0 RBACs %s", got.Error())
	}
}

func TestTooManyRbacs(t *testing.T) {
	_, got := NewAuthorizationEngine([]pb.RBAC{{}, {}, {}})
	if got == nil || !strings.HasSuffix(got.Error(), "code = InvalidArgument desc = must provide 1 or 2 RBACs") {
		t.Errorf("Expected wrong number of RBACs error for 3 RBACs")
	}
}

func TestWrongRbacActions(t *testing.T) {
	_, got := NewAuthorizationEngine([]pb.RBAC{{Action: pb.RBAC_ALLOW}, {Action: pb.RBAC_DENY}})
	if got == nil || !strings.HasSuffix(got.Error(), "code = InvalidArgument desc = when providing 2 RBACs, must have 1 DENY and 1 ALLOW in that order") {
		t.Errorf("Expected wrong RBAC actions error for ALLOW followed by DENY")
	}
}

// func TestExample(t *testing.T) {
// 	condition := expr.Expr{Expr_Kind: }
// 	policy := pb.Policy{Condition: &condition}
// 	rbac := pb.RBAC{Action: pb.RBAC_ALLOW, Policies: map[string]*pb.Policy{"policy1": &policy}}
// 	t.Errorf("rbac: %v", rbac)
// }
