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
)

func TestTooFewRbacs(t *testing.T) {
	want := errWrongNumberRbacs
	_, got := NewCelEvaluationEngine([]pb.RBAC{})
	if got != want {
		t.Errorf("Expected wrong number of RBACs error for 0 RBACs")
	}
}

func TestTooManyRbacs(t *testing.T) {
	want := errWrongNumberRbacs
	_, got := NewCelEvaluationEngine([]pb.RBAC{{}, {}, {}})
	if got != want {
		t.Errorf("Expected wrong number of RBACs error for 3 RBACs")
	}
}

func TestWrongRbacActions(t *testing.T) {
	want := errWrongRbacActions
	_, got := NewCelEvaluationEngine([]pb.RBAC{{Action: pb.RBAC_ALLOW}, {Action: pb.RBAC_DENY}})
	if got != want {
		t.Errorf("Expected wrong RBAC actions error for ALLOW followed by DENY")
	}
}
