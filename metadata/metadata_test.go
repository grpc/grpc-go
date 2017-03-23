/*
 *
 * Copyright 2014, gRPC authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package metadata

import (
	"reflect"
	"testing"
)

const binaryValue = string(128)

func TestEncodeKeyValue(t *testing.T) {
	for _, test := range []struct {
		// input
		kin string
		vin string
		// output
		kout string
		vout string
	}{
		{"key", "abc", "key", "abc"},
		{"KEY", "abc", "key", "abc"},
		{"key-bin", "abc", "key-bin", "YWJj"},
		{"key-bin", binaryValue, "key-bin", "woA="},
	} {
		k, v := encodeKeyValue(test.kin, test.vin)
		if k != test.kout || !reflect.DeepEqual(v, test.vout) {
			t.Fatalf("encodeKeyValue(%q, %q) = %q, %q, want %q, %q", test.kin, test.vin, k, v, test.kout, test.vout)
		}
	}
}

func TestDecodeKeyValue(t *testing.T) {
	for _, test := range []struct {
		// input
		kin string
		vin string
		// output
		kout string
		vout string
		err  error
	}{
		{"a", "abc", "a", "abc", nil},
		{"key-bin", "Zm9vAGJhcg==", "key-bin", "foo\x00bar", nil},
		{"key-bin", "woA=", "key-bin", binaryValue, nil},
		{"a", "abc,efg", "a", "abc,efg", nil},
		{"key-bin", "Zm9vAGJhcg==,Zm9vAGJhcg==", "key-bin", "foo\x00bar,foo\x00bar", nil},
	} {
		k, v, err := DecodeKeyValue(test.kin, test.vin)
		if k != test.kout || !reflect.DeepEqual(v, test.vout) || !reflect.DeepEqual(err, test.err) {
			t.Fatalf("DecodeKeyValue(%q, %q) = %q, %q, %v, want %q, %q, %v", test.kin, test.vin, k, v, err, test.kout, test.vout, test.err)
		}
	}
}

func TestPairsMD(t *testing.T) {
	for _, test := range []struct {
		// input
		kv []string
		// output
		md   MD
		size int
	}{
		{[]string{}, MD{}, 0},
		{[]string{"k1", "v1", "k2-bin", binaryValue}, New(map[string]string{
			"k1":     "v1",
			"k2-bin": binaryValue,
		}), 2},
	} {
		md := Pairs(test.kv...)
		if !reflect.DeepEqual(md, test.md) {
			t.Fatalf("Pairs(%v) = %v, want %v", test.kv, md, test.md)
		}
		if md.Len() != test.size {
			t.Fatalf("Pairs(%v) generates md of size %d, want %d", test.kv, md.Len(), test.size)
		}
	}
}

func TestCopy(t *testing.T) {
	const key, val = "key", "val"
	orig := Pairs(key, val)
	copy := orig.Copy()
	if !reflect.DeepEqual(orig, copy) {
		t.Errorf("copied value not equal to the original, got %v, want %v", copy, orig)
	}
	orig[key][0] = "foo"
	if v := copy[key][0]; v != val {
		t.Errorf("change in original should not affect copy, got %q, want %q", v, val)
	}
}

func TestJoin(t *testing.T) {
	for _, test := range []struct {
		mds  []MD
		want MD
	}{
		{[]MD{}, MD{}},
		{[]MD{Pairs("foo", "bar")}, Pairs("foo", "bar")},
		{[]MD{Pairs("foo", "bar"), Pairs("foo", "baz")}, Pairs("foo", "bar", "foo", "baz")},
		{[]MD{Pairs("foo", "bar"), Pairs("foo", "baz"), Pairs("zip", "zap")}, Pairs("foo", "bar", "foo", "baz", "zip", "zap")},
	} {
		md := Join(test.mds...)
		if !reflect.DeepEqual(md, test.want) {
			t.Errorf("context's metadata is %v, want %v", md, test.want)
		}
	}
}
