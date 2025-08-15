/*
 *
 * Copyright 2019 gRPC authors.
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

package xds

import (
	"reflect"
	"strings"
	"testing"
	"unicode"

	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/clients"
)

const ignorePrefix = "XXX_"

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func ignore(name string) bool {
	if !unicode.IsUpper([]rune(name)[0]) {
		return true
	}
	return strings.HasPrefix(name, ignorePrefix)
}

// A reflection based test to make sure internal.Locality contains all the
// fields (expect for XXX_) from the proto message.
func (s) TestLocalityMatchProtoMessage(t *testing.T) {
	want1 := make(map[string]string)
	for ty, i := reflect.TypeOf(clients.Locality{}), 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if ignore(f.Name) {
			continue
		}
		want1[f.Name] = f.Type.Name()
	}

	want2 := make(map[string]string)
	for ty, i := reflect.TypeOf(corepb.Locality{}), 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if ignore(f.Name) {
			continue
		}
		want2[f.Name] = f.Type.Name()
	}

	if diff := cmp.Diff(want1, want2); diff != "" {
		t.Fatalf("internal type and proto message have different fields: (-got +want):\n%+v", diff)
	}
}

func TestLocalityToAndFromString(t *testing.T) {
	tests := []struct {
		name       string
		localityID clients.Locality
		str        string
		wantErr    bool
	}{
		{
			name:       "3 fields",
			localityID: clients.Locality{Region: "r:r", Zone: "z#z", SubZone: "s^s"},
			str:        `{region="r:r", zone="z#z", sub_zone="s^s"}`,
		},
		{
			name:       "2 fields",
			localityID: clients.Locality{Region: "r:r", Zone: "z#z"},
			str:        `{region="r:r", zone="z#z", sub_zone=""}`,
		},
		{
			name:       "1 field",
			localityID: clients.Locality{Region: "r:r"},
			str:        `{region="r:r", zone="", sub_zone=""}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotStr := LocalityString(tt.localityID)
			if gotStr != tt.str {
				t.Errorf("%#v.String() = %q, want %q", tt.localityID, gotStr, tt.str)
			}

			gotID, err := LocalityFromString(tt.str)
			if (err != nil) != tt.wantErr {
				t.Errorf("clients.LocalityFromString(%q) error = %v, wantErr %v", tt.str, err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(gotID, tt.localityID); diff != "" {
				t.Errorf("clients.LocalityFromString() got = %v, want %v, diff: %s", gotID, tt.localityID, diff)
			}
		})
	}
}
