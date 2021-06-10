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

package balancerconfig

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestDiscoveryMechanismTypeMarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		typ  DiscoveryMechanismType
		want string
	}{
		{
			name: "eds",
			typ:  DiscoveryMechanismTypeEDS,
			want: `"EDS"`,
		},
		{
			name: "dns",
			typ:  DiscoveryMechanismTypeLogicalDNS,
			want: `"LOGICAL_DNS"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := json.Marshal(tt.typ); err != nil || string(got) != tt.want {
				t.Fatalf("DiscoveryMechanismTypeEDS.MarshalJSON() = (%v, %v), want (%s, nil)", string(got), err, tt.want)
			}
		})
	}
}
func TestDiscoveryMechanismTypeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		js      string
		want    DiscoveryMechanismType
		wantErr bool
	}{
		{
			name: "eds",
			js:   `"EDS"`,
			want: DiscoveryMechanismTypeEDS,
		},
		{
			name: "dns",
			js:   `"LOGICAL_DNS"`,
			want: DiscoveryMechanismTypeLogicalDNS,
		},
		{
			name:    "error",
			js:      `"1234"`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got DiscoveryMechanismType
			err := json.Unmarshal([]byte(tt.js), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("DiscoveryMechanismTypeEDS.UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.want); diff != "" {
				t.Fatalf("DiscoveryMechanismTypeEDS.UnmarshalJSON() got unexpected output, diff (-got +want): %v", diff)
			}
		})
	}
}
