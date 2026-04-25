/*
 *
 * Copyright 2026 gRPC authors.
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

package hostname_test

import (
	"testing"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer/hostname"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestHostnameToAndFromEndpoint(t *testing.T) {
	tests := []struct {
		desc            string
		inputHostname   string
		inputAttributes *attributes.Attributes
		wantHostname    string
	}{
		{
			desc:            "empty_attributes",
			inputHostname:   "myservice.example.com",
			inputAttributes: nil,
			wantHostname:    "myservice.example.com",
		},
		{
			desc:            "non-empty_attributes",
			inputHostname:   "myservice.example.com",
			inputAttributes: attributes.New("foo", "bar"),
			wantHostname:    "myservice.example.com",
		},
		{
			desc:            "hostname_not_present_in_empty_attributes",
			inputHostname:   "",
			inputAttributes: nil,
			wantHostname:    "",
		},
		{
			desc:            "hostname_not_present_in_non-empty_attributes",
			inputHostname:   "",
			inputAttributes: attributes.New("foo", "bar"),
			wantHostname:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			endpoint := resolver.Endpoint{Attributes: test.inputAttributes}
			endpoint = hostname.Set(endpoint, test.inputHostname)
			gotHostname := hostname.FromEndpoint(endpoint)
			if gotHostname != test.wantHostname {
				t.Errorf("gotHostname: %v, wantHostname: %v", gotHostname, test.wantHostname)
			}
		})
	}
}
