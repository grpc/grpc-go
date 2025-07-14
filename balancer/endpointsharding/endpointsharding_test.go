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

package endpointsharding

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestRotateEndpoints(t *testing.T) {
	ep := func(addr string) resolver.Endpoint {
		return resolver.Endpoint{Addresses: []resolver.Address{{Addr: addr}}}
	}
	endpoints := []resolver.Endpoint{ep("1"), ep("2"), ep("3"), ep("4"), ep("5")}
	testCases := []struct {
		rval int
		want []resolver.Endpoint
	}{
		{
			rval: 0,
			want: []resolver.Endpoint{ep("1"), ep("2"), ep("3"), ep("4"), ep("5")},
		},
		{
			rval: 1,
			want: []resolver.Endpoint{ep("2"), ep("3"), ep("4"), ep("5"), ep("1")},
		},
		{
			rval: 2,
			want: []resolver.Endpoint{ep("3"), ep("4"), ep("5"), ep("1"), ep("2")},
		},
		{
			rval: 3,
			want: []resolver.Endpoint{ep("4"), ep("5"), ep("1"), ep("2"), ep("3")},
		},
		{
			rval: 4,
			want: []resolver.Endpoint{ep("5"), ep("1"), ep("2"), ep("3"), ep("4")},
		},
	}

	defer func(r func(int) int) {
		randIntN = r
	}(randIntN)

	for _, tc := range testCases {
		t.Run(fmt.Sprint(tc.rval), func(t *testing.T) {
			randIntN = func(int) int {
				return tc.rval
			}
			got := rotateEndpoints(endpoints)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("rand=%v; rotateEndpoints(%v) = %v; want %v", tc.rval, endpoints, got, tc.want)
			}
		})
	}
}
