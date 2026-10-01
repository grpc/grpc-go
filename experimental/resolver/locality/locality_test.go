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

package locality_test

import (
	"testing"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/experimental/resolver/locality"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestLocalityToAndFromResolverState(t *testing.T) {
	tests := []struct {
		desc          string
		inputLocality string
		inputState    resolver.State
		wantLocality  string
	}{
		{
			desc:          "empty_attributes",
			inputLocality: "us-east1",
			inputState:    resolver.State{},
			wantLocality:  "us-east1",
		},
		{
			desc:          "non-empty_attributes",
			inputLocality: "us-east1",
			inputState:    resolver.State{Attributes: attributes.New("foo", "bar")},
			wantLocality:  "us-east1",
		},
		{
			desc:          "locality_not_present_in_empty_attributes",
			inputLocality: "",
			inputState:    resolver.State{},
			wantLocality:  "",
		},
		{
			desc:          "locality_not_present_in_non-empty_attributes",
			inputLocality: "",
			inputState:    resolver.State{Attributes: attributes.New("foo", "bar")},
			wantLocality:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			state := test.inputState
			if test.inputLocality != "" {
				state = locality.Set(test.inputState, test.inputLocality)
			}

			gotLocality := locality.FromResolverState(state)
			if gotLocality != test.wantLocality {
				t.Errorf("locality.FromResolverState(%+v) = %q, want %q", state, gotLocality, test.wantLocality)
			}
		})
	}
}
