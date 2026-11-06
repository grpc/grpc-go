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

package xdsclient

import (
	"bytes"
	"testing"
)

func (s) TestAnyProto(t *testing.T) {
	tests := []struct {
		name    string
		typeURL string
		value   []byte
	}{
		{
			name:    "empty",
			typeURL: "",
			value:   nil,
		},
		{
			name:    "populated",
			typeURL: "type.googleapis.com/test.Resource",
			value:   []byte("serialized-bytes"),
		},
		{
			name:    "empty_value",
			typeURL: "type.googleapis.com/test.Resource",
			value:   []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := AnyProto{
				TypeURL: tt.typeURL,
				Value:   tt.value,
			}
			if got := a.TypeURL; got != tt.typeURL {
				t.Errorf("a.TypeURL = %q, want %q", got, tt.typeURL)
			}
			if !bytes.Equal(a.Value, tt.value) {
				t.Errorf("a.Value = %v, want %v", a.Value, tt.value)
			}
		})
	}
}
