/*
 *
 * Copyright 2014 gRPC authors.
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

package metadata

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
)

const defaultTestTimeout = 10 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestPairsMD(t *testing.T) {
	for _, test := range []struct {
		// input
		kv []string
		// output
		md MD
	}{
		{[]string{}, MD{}},
		{[]string{"k1", "v1", "k1", "v2"}, MD{"k1": []string{"v1", "v2"}}},
	} {
		md := Pairs(test.kv...)
		if !reflect.DeepEqual(md, test.md) {
			t.Fatalf("Pairs(%v) = %v, want %v", test.kv, md, test.md)
		}
	}
}

func (s) TestCopy(t *testing.T) {
	const key, val = "key", "val"
	orig := Pairs(key, val)
	cpy := orig.Copy()
	if !reflect.DeepEqual(orig, cpy) {
		t.Errorf("copied value not equal to the original, got %v, want %v", cpy, orig)
	}
	orig[key][0] = "foo"
	if v := cpy[key][0]; v != val {
		t.Errorf("change in original should not affect copy, got %q, want %q", v, val)
	}
}

func (s) TestJoin(t *testing.T) {
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

func (s) TestGet(t *testing.T) {
	for _, test := range []struct {
		md       MD
		key      string
		wantVals []string
	}{
		{md: Pairs("My-Optional-Header", "42"), key: "My-Optional-Header", wantVals: []string{"42"}},
		{md: Pairs("Header", "42", "Header", "43", "Header", "44", "other", "1"), key: "HEADER", wantVals: []string{"42", "43", "44"}},
		{md: Pairs("HEADER", "10"), key: "HEADER", wantVals: []string{"10"}},
	} {
		vals := test.md.Get(test.key)
		if !reflect.DeepEqual(vals, test.wantVals) {
			t.Errorf("value of metadata %v is %v, want %v", test.key, vals, test.wantVals)
		}
	}
}

func (s) TestSet(t *testing.T) {
	for _, test := range []struct {
		md      MD
		setKey  string
		setVals []string
		want    MD
	}{
		{
			md:      Pairs("My-Optional-Header", "42", "other-key", "999"),
			setKey:  "Other-Key",
			setVals: []string{"1"},
			want:    Pairs("my-optional-header", "42", "other-key", "1"),
		},
		{
			md:      Pairs("My-Optional-Header", "42"),
			setKey:  "Other-Key",
			setVals: []string{"1", "2", "3"},
			want:    Pairs("my-optional-header", "42", "other-key", "1", "other-key", "2", "other-key", "3"),
		},
		{
			md:      Pairs("My-Optional-Header", "42"),
			setKey:  "Other-Key",
			setVals: []string{},
			want:    Pairs("my-optional-header", "42"),
		},
	} {
		test.md.Set(test.setKey, test.setVals...)
		if !reflect.DeepEqual(test.md, test.want) {
			t.Errorf("value of metadata is %v, want %v", test.md, test.want)
		}
	}
}

func (s) TestAppend(t *testing.T) {
	for _, test := range []struct {
		md         MD
		appendKey  string
		appendVals []string
		want       MD
	}{
		{
			md:         Pairs("My-Optional-Header", "42"),
			appendKey:  "Other-Key",
			appendVals: []string{"1"},
			want:       Pairs("my-optional-header", "42", "other-key", "1"),
		},
		{
			md:         Pairs("My-Optional-Header", "42"),
			appendKey:  "my-OptIoNal-HeAder",
			appendVals: []string{"1", "2", "3"},
			want: Pairs("my-optional-header", "42", "my-optional-header", "1",
				"my-optional-header", "2", "my-optional-header", "3"),
		},
		{
			md:         Pairs("My-Optional-Header", "42"),
			appendKey:  "my-OptIoNal-HeAder",
			appendVals: []string{},
			want:       Pairs("my-optional-header", "42"),
		},
	} {
		test.md.Append(test.appendKey, test.appendVals...)
		if !reflect.DeepEqual(test.md, test.want) {
			t.Errorf("value of metadata is %v, want %v", test.md, test.want)
		}
	}
}

func (s) TestDelete(t *testing.T) {
	for _, test := range []struct {
		md        MD
		deleteKey string
		want      MD
	}{
		{
			md:        Pairs("My-Optional-Header", "42"),
			deleteKey: "My-Optional-Header",
			want:      Pairs(),
		},
		{
			md:        Pairs("My-Optional-Header", "42"),
			deleteKey: "Other-Key",
			want:      Pairs("my-optional-header", "42"),
		},
		{
			md:        Pairs("My-Optional-Header", "42"),
			deleteKey: "my-OptIoNal-HeAder",
			want:      Pairs(),
		},
	} {
		test.md.Delete(test.deleteKey)
		if !reflect.DeepEqual(test.md, test.want) {
			t.Errorf("value of metadata is %v, want %v", test.md, test.want)
		}
	}
}

func (s) TestFromIncomingContext(t *testing.T) {
	md := Pairs(
		"X-My-Header-1", "42",
	)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Verify that we lowercase if callers directly modify md
	md["X-INCORRECT-UPPERCASE"] = []string{"foo"}
	ctx = NewIncomingContext(ctx, md)

	result, found := FromIncomingContext(ctx)
	if !found {
		t.Fatal("FromIncomingContext must return metadata")
	}
	expected := MD{
		"x-my-header-1":         []string{"42"},
		"x-incorrect-uppercase": []string{"foo"},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("FromIncomingContext returned %#v, expected %#v", result, expected)
	}

	// ensure modifying result does not modify the value in the context
	result["new_key"] = []string{"foo"}
	result["x-my-header-1"][0] = "mutated"

	result2, found := FromIncomingContext(ctx)
	if !found {
		t.Fatal("FromIncomingContext must return metadata")
	}
	if !reflect.DeepEqual(result2, expected) {
		t.Errorf("FromIncomingContext after modifications returned %#v, expected %#v", result2, expected)
	}
}

func (s) TestValueFromIncomingContext(t *testing.T) {
	md := Pairs(
		"X-My-Header-1", "42",
		"X-My-Header-2", "43-1",
		"X-My-Header-2", "43-2",
		"x-my-header-3", "44",
	)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Verify that we lowercase if callers directly modify md
	md["X-INCORRECT-UPPERCASE"] = []string{"foo"}
	ctx = NewIncomingContext(ctx, md)

	for _, test := range []struct {
		key  string
		want []string
	}{
		{
			key:  "x-my-header-1",
			want: []string{"42"},
		},
		{
			key:  "x-my-header-2",
			want: []string{"43-1", "43-2"},
		},
		{
			key:  "x-my-header-3",
			want: []string{"44"},
		},
		{
			key:  "x-unknown",
			want: nil,
		},
		{
			key:  "x-incorrect-uppercase",
			want: []string{"foo"},
		},
	} {
		v := ValueFromIncomingContext(ctx, test.key)
		if !reflect.DeepEqual(v, test.want) {
			t.Errorf("value of metadata is %v, want %v", v, test.want)
		}
	}
}

func (s) TestAppendToOutgoingContext(t *testing.T) {
	// Pre-existing metadata
	tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx := NewOutgoingContext(tCtx, Pairs("k1", "v1", "k2", "v2"))
	ctx = AppendToOutgoingContext(ctx, "k1", "v3")
	ctx = AppendToOutgoingContext(ctx, "k1", "v4")
	md, ok := FromOutgoingContext(ctx)
	if !ok {
		t.Errorf("Expected MD to exist in ctx, but got none")
	}
	want := Pairs("k1", "v1", "k1", "v3", "k1", "v4", "k2", "v2")
	if !reflect.DeepEqual(md, want) {
		t.Errorf("context's metadata is %v, want %v", md, want)
	}

	// No existing metadata
	ctx = AppendToOutgoingContext(tCtx, "k1", "v1")
	md, ok = FromOutgoingContext(ctx)
	if !ok {
		t.Errorf("Expected MD to exist in ctx, but got none")
	}
	want = Pairs("k1", "v1")
	if !reflect.DeepEqual(md, want) {
		t.Errorf("context's metadata is %v, want %v", md, want)
	}
}

func (s) TestAppendToOutgoingContext_Repeated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	for i := 0; i < 100; i = i + 2 {
		ctx1 := AppendToOutgoingContext(ctx, "k", strconv.Itoa(i))
		ctx2 := AppendToOutgoingContext(ctx, "k", strconv.Itoa(i+1))

		md1, _ := FromOutgoingContext(ctx1)
		md2, _ := FromOutgoingContext(ctx2)

		if reflect.DeepEqual(md1, md2) {
			t.Fatalf("md1, md2 = %v, %v; should not be equal", md1, md2)
		}

		ctx = ctx1
	}
}

func (s) TestAppendToOutgoingContext_FromKVSlice(t *testing.T) {
	const k, v = "a", "b"
	kv := []string{k, v}
	tCtx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx := AppendToOutgoingContext(tCtx, kv...)
	md, _ := FromOutgoingContext(ctx)
	if md[k][0] != v {
		t.Fatalf("md[%q] = %q; want %q", k, md[k], v)
	}
	kv[1] = "xxx"
	md, _ = FromOutgoingContext(ctx)
	if md[k][0] != v {
		t.Fatalf("md[%q] = %q; want %q", k, md[k], v)
	}
}

// Old/slow approach to adding metadata to context
func Benchmark_AddingMetadata_ContextManipulationApproach(b *testing.B) {
	// TODO: Add in N=1-100 tests once Go1.6 support is removed.
	const num = 10
	for n := 0; n < b.N; n++ {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		for i := 0; i < num; i++ {
			md, _ := FromOutgoingContext(ctx)
			NewOutgoingContext(ctx, Join(Pairs("k1", "v1", "k2", "v2"), md))
		}
	}
}

// Newer/faster approach to adding metadata to context
func BenchmarkAppendToOutgoingContext(b *testing.B) {
	const num = 10
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for n := 0; n < b.N; n++ {
		for i := 0; i < num; i++ {
			ctx = AppendToOutgoingContext(ctx, "k1", "v1", "k2", "v2")
		}
	}
}

func BenchmarkFromOutgoingContext(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	ctx = NewOutgoingContext(ctx, MD{"k3": {"v3", "v4"}})
	ctx = AppendToOutgoingContext(ctx, "k1", "v1", "k2", "v2")

	for n := 0; n < b.N; n++ {
		FromOutgoingContext(ctx)
	}
}

func BenchmarkFromIncomingContext(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	md := Pairs("X-My-Header-1", "42")
	ctx = NewIncomingContext(ctx, md)

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		FromIncomingContext(ctx)
	}
}

func BenchmarkValueFromIncomingContext(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	md := Pairs("X-My-Header-1", "42")
	ctx = NewIncomingContext(ctx, md)

	b.Run("key-found", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			result := ValueFromIncomingContext(ctx, "x-my-header-1")
			if len(result) != 1 {
				b.Fatal("ensures not optimized away")
			}
		}
	})

	b.Run("key-not-found", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			result := ValueFromIncomingContext(ctx, "key-not-found")
			if len(result) != 0 {
				b.Fatal("ensures not optimized away")
			}
		}
	})
}

// TestString verifies that String shows keys and values only for keys known to
// be safe to log, omits every other key (including its name) and reports only
// the count of omitted keys, and never panics on nil/empty inputs.
func (s) TestString(t *testing.T) {
	tests := []struct {
		name string
		md   MD
		want string
	}{
		{
			name: "empty",
			md:   MD{},
			want: "map[]",
		},
		{
			name: "nil",
			md:   nil,
			want: "map[]",
		},
		{
			name: "safe-key-shown",
			md:   Pairs("content-type", "application/grpc"),
			want: "map[content-type:[application/grpc]]",
		},
		{
			// A safe key with multiple values shows all of them.
			name: "safe-key-multi-value",
			md:   Pairs("grpc-accept-encoding", "gzip", "grpc-accept-encoding", "snappy"),
			want: "map[grpc-accept-encoding:[gzip snappy]]",
		},
		{
			// nil/empty value slices must not panic; a safe key shows [].
			name: "safe-key-nil-value",
			md:   MD{"content-type": nil},
			want: "map[content-type:[]]",
		},
		{
			// A redacted key with a nil value slice still redacts, no panic.
			name: "redacted-key-nil-value",
			md:   MD{"authorization": nil},
			want: "map[<1 redacted>]",
		},
		{
			// Bracket characters in a safe value are passed through verbatim.
			name: "safe-value-with-brackets",
			md:   MD{"content-type": []string{"a]b ["}},
			want: "map[content-type:[a]b []]",
		},
		{
			// Keys differing only in case are sorted byte-wise (uppercase
			// first); both still match the case-insensitive safelist.
			name: "case-only-collision",
			md:   MD{"Content-Type": []string{"a"}, "content-type": []string{"b"}},
			want: "map[Content-Type:[a] content-type:[b]]",
		},
		{
			// A sensitive key is omitted entirely, including its name; only the
			// count is reported.
			name: "sensitive-key-redacted",
			md:   Pairs("authorization", "Bearer super-secret-token"),
			want: "map[<1 redacted>]",
		},
		{
			// Application-defined keys are omitted; the key name (which may
			// itself be sensitive) is never shown.
			name: "custom-key-redacted",
			md:   Pairs("x-my-app-secret", "hunter2"),
			want: "map[<1 redacted>]",
		},
		{
			// A single key with multiple values counts as one redacted key, and
			// the value count is not leaked.
			name: "multi-value-redacted",
			md:   Pairs("cookie", "a=1", "cookie", "b=2"),
			want: "map[<1 redacted>]",
		},
		{
			// Multiple redacted keys are reported by count only.
			name: "multiple-redacted-keys",
			md:   Pairs("authorization", "tok", "cookie", "x"),
			want: "map[<2 redacted>]",
		},
		{
			// Safe keys are shown (sorted) and non-safe keys are summarized by a
			// trailing count.
			name: "mixed-safe-and-redacted",
			md:   Pairs("grpc-encoding", "gzip", "authorization", "tok", "user-agent", "grpc-go/x"),
			want: "map[grpc-encoding:[gzip] user-agent:[grpc-go/x] <1 redacted>]",
		},
		{
			// Keys built via map literal are not canonicalized to lowercase;
			// the safelist match is case-insensitive so a safe key is still
			// shown.
			name: "uppercase-safe-key-shown",
			md:   MD{"Content-Type": []string{"application/grpc"}},
			want: "map[Content-Type:[application/grpc]]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.md.String(); got != test.want {
				t.Errorf("MD.String() = %q, want %q", got, test.want)
			}
		})
	}
}
