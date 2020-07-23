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
			if gotErr == nil || !strings.HasSuffix(gotErr.Error(), tc.wantErr) {
				t.Errorf(tc.errStr)
			}
		})
	}
}
