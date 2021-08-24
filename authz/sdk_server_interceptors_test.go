/*
 *
 * Copyright 2021 gRPC authors.
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

package authz_test

import (
	"testing"

	"google.golang.org/grpc/authz"
)

func TestNewStatic(t *testing.T) {
	tests := map[string]struct {
		authzPolicy string
		wantErr     bool
	}{
		"InvalidPolicyFailsToCreateInterceptor": {
			authzPolicy: `{}`,
			wantErr:     true,
		},
		"ValidPolicyCreatesInterceptor": {
			authzPolicy: `{		
				"name": "authz",
				"allow_rules": 
				[
					{
						"name": "allow_all"
					}
				]
			}`,
			wantErr: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := authz.NewStatic(test.authzPolicy); (err != nil) != test.wantErr {
				t.Fatalf("NewStatic(%v) returned err: %v, want err: %v", test.authzPolicy, err, test.wantErr)
			}
		})
	}
}
