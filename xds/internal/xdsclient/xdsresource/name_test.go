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
 */

package xdsresource

import (
	"reflect"
	"testing"

	"google.golang.org/grpc/internal/envconfig"
)

func TestParseName(t *testing.T) {
	tests := []struct {
		name string
		env  bool // Whether federation env is set to true.
		in   string
		want *Name
	}{
		{
			name: "env off",
			env:  false,
			in:   "xdstp://auth/type/id",
			want: &Name{ID: "xdstp://auth/type/id"},
		},
		{
			name: "old style name",
			env:  true,
			in:   "test-resource",
			want: &Name{ID: "test-resource"},
		},
		{
			name: "invalid not url",
			env:  true,
			in:   "a:/b/c",
			want: &Name{ID: "a:/b/c"},
		},
		{
			name: "invalid no resource type",
			env:  true,
			in:   "xdstp://auth/id",
			want: &Name{ID: "xdstp://auth/id"},
		},
		{
			name: "valid no ctx params",
			env:  true,
			in:   "xdstp://auth/type/id",
			want: &Name{Scheme: "xdstp", Authority: "auth", Type: "type", ID: "id"},
		},
		{
			name: "valid with ctx params",
			env:  true,
			in:   "xdstp://auth/type/id?a=1&b=2",
			want: &Name{Scheme: "xdstp", Authority: "auth", Type: "type", ID: "id", ContextParams: map[string]string{"a": "1", "b": "2"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env {
				defer func() func() {
					oldEnv := envconfig.XDSFederation
					envconfig.XDSFederation = true
					return func() { envconfig.XDSFederation = oldEnv }
				}()()
			}
			if got := ParseName(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseName() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestNameStringCtxParamsOrder covers the case that if two names differ only in
// context parameter __order__, the parsed name.String() has the same value.
func TestNameStringCtxParamsOrder(t *testing.T) {
	oldEnv := envconfig.XDSFederation
	envconfig.XDSFederation = true
	defer func() { envconfig.XDSFederation = oldEnv }()

	const (
		a = "xdstp://auth/type/id?a=1&b=2"
		b = "xdstp://auth/type/id?b=2&a=1"
	)
	aParsed := ParseName(a).String()
	bParsed := ParseName(b).String()

	if aParsed != bParsed {
		t.Fatalf("aParsed.String() = %q, bParsed.String() = %q, want them to be the same", aParsed, bParsed)
	}
}
