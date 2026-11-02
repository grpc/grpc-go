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

package serviceconfig

import (
	"encoding/json"
	"testing"
)

func TestUint32_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Uint32
		wantErr bool
	}{
		{
			name:  "Number",
			input: `42`,
			want:  42,
		},
		{
			name:  "String",
			input: `"42"`,
			want:  42,
		},
		{
			name:  "Uint32Maximum",
			input: `"4294967295"`,
			want:  4294967295,
		},
		{
			name:  "FractionalLeadingZerosWithExponent",
			input: `"0.00000000001e11"`,
			want:  1,
		},
		{
			name:  "NegativeZero",
			input: `"-0"`,
			want:  0,
		},
		{
			name:  "NegativeZeroWithOversizedExponent",
			input: `"-0e999999999999999999999"`,
			want:  0,
		},
		{
			name:  "ZeroWithOversizedExponent",
			input: `"0e999999999999999999999"`,
			want:  0,
		},
		{
			name:    "OversizedExponent",
			input:   `"1e999999999999999999999"`,
			wantErr: true,
		},
		{
			name:  "QuotedExponent",
			input: `"1e2"`,
			want:  100,
		},
		{
			name:  "QuotedDecimalExponent",
			input: `"1.024e3"`,
			want:  1024,
		},
		{
			name:  "QuotedNegativeExponent",
			input: `"100e-2"`,
			want:  1,
		},
		{
			name:    "NegativeExponentBelowOne",
			input:   `"1e-2"`,
			wantErr: true,
		},
		{
			name:    "NegativeExponentNonzeroFraction",
			input:   `"150e-2"`,
			wantErr: true,
		},
		{
			name:    "QuotedCommaSeparatedValue",
			input:   `"1,000"`,
			wantErr: true,
		},
		{
			name:  "QuotedZeroFraction",
			input: `"1.0"`,
			want:  1,
		},
		{
			name:    "Negative",
			input:   `"-1"`,
			wantErr: true,
		},
		{
			name:    "Uint32Overflow",
			input:   `"4294967296"`,
			wantErr: true,
		},
		{
			name:    "NonzeroFraction",
			input:   `"1.5"`,
			wantErr: true,
		},
		{
			name:    "EmptyString",
			input:   `""`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got Uint32
			err := json.Unmarshal([]byte(test.input), &got)
			if (err != nil) != test.wantErr {
				t.Fatalf("json.Unmarshal(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("json.Unmarshal(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestUint32_UnmarshalJSONNullKeepsValue(t *testing.T) {
	got := Uint32(42)
	if err := json.Unmarshal([]byte("null"), &got); err != nil {
		t.Fatalf("json.Unmarshal(null) error = %v, want nil", err)
	}
	if got != 42 {
		t.Fatalf("UnmarshalJSON(null) changed value to %d, want 42", got)
	}
}
