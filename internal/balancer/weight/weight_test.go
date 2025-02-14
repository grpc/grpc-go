/*
 *
 * Copyright 2025 gRPC authors.
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

package weight_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/internal/balancer/weight"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestAddrInfoToAndFromAttributes(t *testing.T) {
	tests := []struct {
		desc             string
		inputAddrInfo    weight.EndpointInfo
		inputAttributes  *attributes.Attributes
		wantEndpointInfo weight.EndpointInfo
	}{
		{
			desc:             "empty attributes",
			inputAddrInfo:    weight.EndpointInfo{Weight: 100},
			inputAttributes:  nil,
			wantEndpointInfo: weight.EndpointInfo{Weight: 100},
		},
		{
			desc:             "non-empty attributes",
			inputAddrInfo:    weight.EndpointInfo{Weight: 100},
			inputAttributes:  attributes.New("foo", "bar"),
			wantEndpointInfo: weight.EndpointInfo{Weight: 100},
		},
		{
			desc:             "endpointInfo not present in empty attributes",
			inputAddrInfo:    weight.EndpointInfo{},
			inputAttributes:  nil,
			wantEndpointInfo: weight.EndpointInfo{},
		},
		{
			desc:             "endpointInfo not present in non-empty attributes",
			inputAddrInfo:    weight.EndpointInfo{},
			inputAttributes:  attributes.New("foo", "bar"),
			wantEndpointInfo: weight.EndpointInfo{},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			endpoint := resolver.Endpoint{Attributes: test.inputAttributes}
			endpoint = weight.Set(endpoint, test.inputAddrInfo)
			gotEndpointInfo := weight.ForEndpoint(endpoint)
			if !cmp.Equal(gotEndpointInfo, test.wantEndpointInfo) {
				t.Errorf("gotEndpointInfo: %v, wantEndpointInfo: %v", gotEndpointInfo, test.wantEndpointInfo)
			}

		})
	}
}

func (s) TestEndpointInfoEmpty(t *testing.T) {
	ep := resolver.Endpoint{}
	gotEndpointInfo := weight.ForEndpoint(ep)
	wantEndpointInfo := weight.EndpointInfo{}
	if !cmp.Equal(gotEndpointInfo, wantEndpointInfo) {
		t.Errorf("gotEndpointInfo: %v, wantEndpointInfo: %v", gotEndpointInfo, wantEndpointInfo)
	}
}
