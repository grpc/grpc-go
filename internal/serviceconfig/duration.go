/*
 *
 * Copyright 2023 gRPC authors.
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
	"math"
	"strconv"
	"strings"
	"time"
)

// Duration defines JSON marshal and unmarshal methods to conform to the
// protobuf JSON spec defined [here].
//
// [here]: https://protobuf.dev/reference/protobuf/google.protobuf/#duration
type Duration time.Duration

func (d Duration) String() string {
	return fmt.Sprint(time.Duration(d))
}

// MarshalJSON converts from d to a JSON string output.
func (d Duration) MarshalJSON() ([]byte, error) {
	ns := time.Duration(d).Nanoseconds()
	sec := ns / int64(time.Second)
	ns = ns % int64(time.Second)

	var sign string
	if sec < 0 {
		sign, sec, ns = "-", -1*sec, -1*ns
	}

	// Generated output always contains 0, 3, 6, or 9 fractional digits,
	// depending on required precision.
	str := fmt.Sprintf("%s%d.%09d", sign, sec, ns)
	str = strings.TrimSuffix(str, "000")
	str = strings.TrimSuffix(str, "000")
	str = strings.TrimSuffix(str, ".000")
	return []byte(fmt.Sprintf("\"%ss\"", str)), nil
}

// UnmarshalJSON unmarshals b as a duration JSON string into d.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if !strings.HasSuffix(s, "s") {
		return fmt.Errorf("malformed duration %q", s)
	}
	ss := strings.SplitN(s[:len(s)-1], ".", 3)
	if len(ss) > 2 {
		return fmt.Errorf("malformed duration %q", s)
	}
	// hasDigits is set if either the whole or fractional part of the number is
	// present, since both are optional but one is required.
	hasDigits := false
	if len(ss[0]) > 0 {
		sec, err := strconv.ParseInt(ss[0], 10, 64)
		if err != nil {
			return fmt.Errorf("malformed duration %q: %v", s, err)
		}
		const maxSeconds = math.MaxInt64 / int64(time.Second)
		const minSeconds = math.MinInt64 / int64(time.Second)
		if sec > maxSeconds || sec < minSeconds {
			return fmt.Errorf("out of range: %q", s)
		}
		*d = Duration(sec) * Duration(time.Second)
		hasDigits = true
	} else {
		*d = 0
	}
	if len(ss) == 2 && len(ss[1]) > 0 {
		if len(ss[1]) > 9 {
			return fmt.Errorf("malformed duration %q", s)
		}
		f, err := strconv.ParseInt(ss[1], 10, 64)
		if err != nil {
			return fmt.Errorf("malformed duration %q: %v", s, err)
		}
		neg := false
		if *d < 0 {
			neg = true
			f *= -1
		}
		for i := 9; i > len(ss[1]); i-- {
			f *= 10
		}
		*d += Duration(f)
		if neg != (*d < 0) {
			return fmt.Errorf("out of range: %q", s)
		}
		hasDigits = true
	}
	if !hasDigits {
		return fmt.Errorf("malformed duration %q", s)
	}
	return nil
}
