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

package resolver

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestValidateEndpoints tests different scenarios of resolver addresses being
// validated by the ValidateEndpoint helper.
func (s) TestValidateEndpoints(t *testing.T) {
	addr1 := Address{Addr: "addr1"}
	addr2 := Address{Addr: "addr2"}
	addr3 := Address{Addr: "addr3"}
	addr4 := Address{Addr: "addr4"}
	tests := []struct {
		name      string
		endpoints []Endpoint
		wantErr   bool
	}{
		{
			name: "duplicate-address-across-endpoints",
			endpoints: []Endpoint{
				{Addresses: []Address{addr1}},
				{Addresses: []Address{addr1}},
			},
			wantErr: false,
		},
		{
			name: "duplicate-address-same-endpoint",
			endpoints: []Endpoint{
				{Addresses: []Address{addr1, addr1}},
			},
			wantErr: false,
		},
		{
			name: "duplicate-address-across-endpoints-plural-addresses",
			endpoints: []Endpoint{
				{Addresses: []Address{addr1, addr2, addr3}},
				{Addresses: []Address{addr3, addr4}},
			},
			wantErr: false,
		},
		{
			name: "no-shared-addresses",
			endpoints: []Endpoint{
				{Addresses: []Address{addr1, addr2}},
				{Addresses: []Address{addr3, addr4}},
			},
			wantErr: false,
		},
		{
			name: "endpoint-with-no-addresses",
			endpoints: []Endpoint{
				{Addresses: []Address{addr1, addr2}},
				{Addresses: []Address{}},
			},
			wantErr: false,
		},
		{
			name:      "empty-endpoints-list",
			endpoints: []Endpoint{},
			wantErr:   true,
		},
		{
			name:      "endpoint-list-with-no-addresses",
			endpoints: []Endpoint{{}, {}},
			wantErr:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateEndpoints(test.endpoints)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateEndpoints() wantErr: %v, got: %v", test.wantErr, err)
			}
		})
	}
}
