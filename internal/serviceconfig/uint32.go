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
	"fmt"
	"strconv"
	"strings"
)

// Uint32 decodes service config fields whose protobuf schema type is uint32.
// ProtoJSON accepts JSON numbers and quoted numbers (including integral decimal
// and exponent forms) for these fields. A JSON null leaves the value unchanged.
//
// protojson cannot decode a scalar directly; it requires a protobuf message.
// Using wrapperspb.UInt32Value would add wrapperspb to the grpc package's
// dependency graph, while borrowing an unrelated generated message would
// introduce an undesirable package coupling. Keep this local decoder so
// service config parsing preserves ProtoJSON uint32 semantics without changing
// the grpc package's dependency graph.
//
// normalizeJSONUint32 also preserves ProtoJSON edge cases such as negative zero
// and zero with an exponent too large to fit in an integer. A big.Float-based
// parser was avoided because it rejects the latter even though ProtoJSON accepts
// it as zero. Malformed strings, non-integral values, negative nonzero values,
// and values outside the uint32 range are rejected.
type Uint32 uint32

// UnmarshalJSON unmarshals b as a JSON number or quoted number into u. A
// JSON null leaves u unchanged.
func (u *Uint32) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(b, &number); err != nil {
		return err
	}

	value, err := parseJSONUint32(number.String())
	if err != nil {
		return err
	}
	*u = Uint32(value)
	return nil
}

func parseJSONUint32(s string) (uint32, error) {
	value, ok := normalizeJSONUint32(s)
	if !ok {
		return 0, fmt.Errorf("invalid uint32 value %q", s)
	}
	return value, nil
}

func normalizeJSONUint32(number string) (uint32, bool) {
	if number == "" {
		return 0, false
	}

	// ProtoJSON accepts negative zero for unsigned values, but no other
	// negative values.
	if number[0] == '-' {
		return 0, isJSONZero(number[1:])
	}

	// Zero is valid even with an exponent too large to fit in an integer.
	if isJSONZero(number) {
		return 0, true
	}

	exponentText := ""
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		exponentText = number[i+1:]
		number = number[:i]
	}

	integerPart := number
	fractionalPart := ""
	if i := strings.IndexByte(number, '.'); i >= 0 {
		integerPart = number[:i]
		fractionalPart = strings.TrimRight(number[i+1:], "0")
	}

	// ProtoJSON drops a leading zero before applying the digit limit below.
	if integerPart == "0" {
		integerPart = ""
	}

	exponent := 0
	if exponentText != "" {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return 0, false
		}
		exponent = int(parsed)
	}

	var digits string
	if exponent >= 0 {
		// Match ProtoJSON's bounded normalization before applying the final
		// uint32 range check.
		const maxDigits = 20
		if len(fractionalPart) > exponent ||
			len(integerPart) > maxDigits ||
			exponent > maxDigits-len(integerPart) {
			return 0, false
		}

		digits = integerPart + fractionalPart +
			strings.Repeat("0", exponent-len(fractionalPart))
	} else {
		if len(fractionalPart) > 0 {
			return 0, false
		}

		decimalPoint := len(integerPart) + exponent
		if decimalPoint < 0 {
			return 0, false
		}

		for _, digit := range integerPart[decimalPoint:] {
			if digit != '0' {
				return 0, false
			}
		}
		digits = integerPart[:decimalPoint]
	}

	if digits == "" {
		return 0, true
	}

	value, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(value), true
}

// isJSONZero reports whether an unsigned JSON number is zero, ignoring any
// exponent. number has already been validated as a JSON number by encoding/json.
func isJSONZero(number string) bool {
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		number = number[:i]
	}
	return number != "" && strings.Trim(number, "0.") == ""
}
