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
 *
 */

/*
Package flags provide convenience types and routines to accept specific types
of flag values on the command line.
*/
package flags

import (
	"bytes"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// StringFlagWithAllowedValues represents a string flag which can only take a
// predefined set of values.
type StringFlagWithAllowedValues struct {
	val     string
	allowed []string
}

var _ flag.Value = (*StringFlagWithAllowedValues)(nil)

// StringWithAllowedValues returns a flag variable of type
// StringFlagWithAllowedValues configured with the provided parameters.
// 'allowed` is the set of values that this flag can be set to.
func StringWithAllowedValues(name, defaultVal, usage string, allowed []string) *StringFlagWithAllowedValues {
	as := &StringFlagWithAllowedValues{defaultVal, allowed}
	StringWithAllowedValuesVar(flag.CommandLine, as, name, defaultVal, usage, allowed)
	return as
}

// StringWithAllowedValuesVar is similar to StringWithAllowedValues except that
// the flag value is stored in the provided 'as' argument.
func StringWithAllowedValuesVar(f *flag.FlagSet, as *StringFlagWithAllowedValues, name, defaultVal, usage string, allowed []string) {
	f.Var(as, name, usage)
}

// Get implements the flag.Getter interface.
func (as *StringFlagWithAllowedValues) Get() interface{} {
	return as
}

// String implements the flag.Value interface.
func (as *StringFlagWithAllowedValues) String() string {
	return as.val
}

// Set implements the flag.Value interface.
func (as *StringFlagWithAllowedValues) Set(val string) error {
	for _, a := range as.allowed {
		if a == val {
			as.val = val
			return nil
		}
	}
	return fmt.Errorf("want one of: %v", strings.Join(as.allowed, ", "))
}

type durationSliceValue []time.Duration

var _ flag.Value = (*durationSliceValue)(nil)

func newDurationSliceValue(val []time.Duration, ds *[]time.Duration) *durationSliceValue {
	*ds = val
	return (*durationSliceValue)(ds)
}

// DurationSlice returns a flag representing a slice of time.Duration objects.
func DurationSlice(name string, defaultVal []time.Duration, usage string) *[]time.Duration {
	ds := new([]time.Duration)
	DurationSliceVar(flag.CommandLine, ds, name, defaultVal, usage)
	return ds
}

// DurationSliceVar is similar to DurationSlice except that is returns the flag
// in the passed in 'ds' argument.
func DurationSliceVar(f *flag.FlagSet, ds *[]time.Duration, name string, defaultVal []time.Duration, usage string) {
	val := append([]time.Duration(nil), defaultVal...)
	dsv := newDurationSliceValue(val, ds)
	f.Var(dsv, name, usage)
}

// Get implements the flag.Getter interface.
func (dsv *durationSliceValue) Get() interface{} {
	return []time.Duration(*dsv)
}

// Set implements the flag.Value interface.
func (dsv *durationSliceValue) Set(s string) error {
	ds := strings.Split(s, ",")
	var dd []time.Duration
	for _, n := range ds {
		d, err := time.ParseDuration(n)
		if err != nil {
			return err
		}
		dd = append(dd, d)
	}
	*dsv = durationSliceValue(dd)
	return nil
}

// String implements the flag.Value interface.
func (dsv *durationSliceValue) String() string {
	var b bytes.Buffer
	for i, d := range *dsv {
		if i > 0 {
			b.WriteRune(',')
		}
		b.WriteString(d.String())
	}
	return b.String()
}

type intSliceValue []int

var _ flag.Value = (*intSliceValue)(nil)

func newIntSliceValue(val []int, is *[]int) *intSliceValue {
	*is = val
	return (*intSliceValue)(is)
}

// IntSlice returns a flag representing a slice of ints.
func IntSlice(name string, defaultVal []int, usage string) *[]int {
	is := new([]int)
	IntSliceVar(flag.CommandLine, is, name, defaultVal, usage)
	return is
}

// IntSliceVar is similar to IntSlice except that is returns the flag in the
// passed in 'is' argument.
func IntSliceVar(f *flag.FlagSet, is *[]int, name string, defaultVal []int, usage string) {
	val := append([]int(nil), defaultVal...)
	isv := newIntSliceValue(val, is)
	f.Var(isv, name, usage)
}

// Get implements the flag.Getter interface.
func (isv *intSliceValue) Get() interface{} {
	return []int(*isv)
}

// Set implements the flag.Value interface.
func (isv *intSliceValue) Set(s string) error {
	is := strings.Split(s, ",")
	var ret []int
	for _, n := range is {
		i, err := strconv.Atoi(n)
		if err != nil {
			return err
		}
		ret = append(ret, i)
	}
	*isv = intSliceValue(ret)
	return nil
}

// String implements the flag.Value interface.
func (isv *intSliceValue) String() string {
	var b bytes.Buffer
	for i, n := range *isv {
		if i > 0 {
			b.WriteRune(',')
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}
