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

// Package metadata define the structure of the metadata supported by gRPC library.
// Please refer to https://github.com/grpc/grpc/blob/master/doc/PROTOCOL-HTTP2.md
// for more information about custom-metadata.
package metadata // import "google.golang.org/grpc/metadata"

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"google.golang.org/grpc/internal"
)

func init() {
	internal.FromOutgoingContextRaw = fromOutgoingContextRaw
}

// DecodeKeyValue returns k, v, nil.
//
// Deprecated: use k and v directly instead.
func DecodeKeyValue(k, v string) (string, string, error) {
	return k, v, nil
}

// MD is a mapping from metadata keys to values. Users should use the following
// two convenience functions New and Pairs to generate MD.
type MD map[string][]string

// New creates an MD from a given key-value map.
//
// Only the following ASCII characters are allowed in keys:
//   - digits: 0-9
//   - uppercase letters: A-Z (normalized to lower)
//   - lowercase letters: a-z
//   - special characters: -_.
//
// Uppercase letters are automatically converted to lowercase.
//
// Keys beginning with "grpc-" are reserved for grpc-internal use only and may
// result in errors if set in metadata.
func New(m map[string]string) MD {
	md := make(MD, len(m))
	for k, val := range m {
		key := strings.ToLower(k)
		md[key] = append(md[key], val)
	}
	return md
}

// Pairs returns an MD formed by the mapping of key, value ...
// Pairs panics if len(kv) is odd.
//
// Only the following ASCII characters are allowed in keys:
//   - digits: 0-9
//   - uppercase letters: A-Z (normalized to lower)
//   - lowercase letters: a-z
//   - special characters: -_.
//
// Uppercase letters are automatically converted to lowercase.
//
// Keys beginning with "grpc-" are reserved for grpc-internal use only and may
// result in errors if set in metadata.
func Pairs(kv ...string) MD {
	if len(kv)%2 == 1 {
		panic(fmt.Sprintf("metadata: Pairs got the odd number of input pairs for metadata: %d", len(kv)))
	}
	md := make(MD, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key := strings.ToLower(kv[i])
		md[key] = append(md[key], kv[i+1])
	}
	return md
}

// Len returns the number of items in md.
func (md MD) Len() int {
	return len(md)
}

// Copy returns a copy of md.
func (md MD) Copy() MD {
	out := make(MD, len(md))
	for k, v := range md {
		out[k] = copyOf(v)
	}
	return out
}

// Get obtains the values for a given key.
//
// k is converted to lowercase before searching in md.
func (md MD) Get(k string) []string {
	k = strings.ToLower(k)
	return md[k]
}

// Set sets the value of a given key with a slice of values.
//
// k is converted to lowercase before storing in md.
func (md MD) Set(k string, vals ...string) {
	if len(vals) == 0 {
		return
	}
	k = strings.ToLower(k)
	md[k] = vals
}

// Append adds the values to key k, not overwriting what was already stored at
// that key.
//
// k is converted to lowercase before storing in md.
func (md MD) Append(k string, vals ...string) {
	if len(vals) == 0 {
		return
	}
	k = strings.ToLower(k)
	md[k] = append(md[k], vals...)
}

// Delete removes the values for a given key k which is converted to lowercase
// before removing it from md.
func (md MD) Delete(k string) {
	k = strings.ToLower(k)
	delete(md, k)
}

// Join joins any number of mds into a single MD.
//
// The order of values for each key is determined by the order in which the mds
// containing those values are presented to Join.
func Join(mds ...MD) MD {
	out := MD{}
	for _, md := range mds {
		for k, v := range md {
			out[k] = append(out[k], v...)
		}
	}
	return out
}

type mdIncomingKey struct{}
type mdOutgoingKey struct{}

// NewIncomingContext creates a new context with incoming md attached. md must
// not be modified after calling this function.
func NewIncomingContext(ctx context.Context, md MD) context.Context {
	return context.WithValue(ctx, mdIncomingKey{}, md)
}

// NewOutgoingContext creates a new context with outgoing md attached. If used
// in conjunction with AppendToOutgoingContext, NewOutgoingContext will
// overwrite any previously-appended metadata. md must not be modified after
// calling this function.
func NewOutgoingContext(ctx context.Context, md MD) context.Context {
	return context.WithValue(ctx, mdOutgoingKey{}, rawMD{md: md})
}

// AppendToOutgoingContext returns a new context with the provided kv merged
// with any existing metadata in the context. Please refer to the documentation
// of Pairs for a description of kv.
func AppendToOutgoingContext(ctx context.Context, kv ...string) context.Context {
	if len(kv)%2 == 1 {
		panic(fmt.Sprintf("metadata: AppendToOutgoingContext got an odd number of input pairs for metadata: %d", len(kv)))
	}
	md, _ := ctx.Value(mdOutgoingKey{}).(rawMD)
	kvCopy := make([]string, len(kv))
	for i := 0; i < len(kv); i += 2 {
		kvCopy[i] = strings.ToLower(kv[i])
		kvCopy[i+1] = kv[i+1]
	}
	return context.WithValue(ctx, mdOutgoingKey{}, rawMD{
		md:    md.md,
		added: &deltaKV{kv: kvCopy, prev: md.added},
	})
}

// FromIncomingContext returns the incoming metadata in ctx if it exists.
//
// All keys in the returned MD are lowercase.
func FromIncomingContext(ctx context.Context) (MD, bool) {
	md, ok := ctx.Value(mdIncomingKey{}).(MD)
	if !ok {
		return nil, false
	}
	out := make(MD, len(md))
	for k, v := range md {
		// We need to manually convert all keys to lower case, because MD is a
		// map, and there's no guarantee that the MD attached to the context is
		// created using our helper functions.
		key := strings.ToLower(k)
		out[key] = copyOf(v)
	}
	return out, true
}

// ValueFromIncomingContext returns the metadata value corresponding to the metadata
// key from the incoming metadata if it exists. Keys are matched in a case insensitive
// manner.
func ValueFromIncomingContext(ctx context.Context, key string) []string {
	md, ok := ctx.Value(mdIncomingKey{}).(MD)
	if !ok {
		return nil
	}

	if v, ok := md[key]; ok {
		return copyOf(v)
	}
	for k, v := range md {
		// Case insensitive comparison: MD is a map, and there's no guarantee
		// that the MD attached to the context is created using our helper
		// functions.
		if strings.EqualFold(k, key) {
			return copyOf(v)
		}
	}
	return nil
}

func copyOf(v []string) []string {
	vals := make([]string, len(v))
	copy(vals, v)
	return vals
}

// fromOutgoingContextRaw returns the un-merged, intermediary contents of rawMD.
//
// Remember to perform strings.ToLower on the keys in the returned MD (MD is a
// map, there's no guarantee it's created using our helper functions).
// Keys yielded by the iterator are already lowercase as AppendToOutgoingContext
// normalizes them.
func fromOutgoingContextRaw(ctx context.Context) (MD, iter.Seq2[string, string], bool) {
	raw, ok := ctx.Value(mdOutgoingKey{}).(rawMD)
	if !ok {
		return nil, nil, false
	}
	if raw.added == nil {
		return raw.md, nil, true
	}
	// Count nodes to pre-allocate the reversal slice.
	n := 0
	for d := raw.added; d != nil; d = d.prev {
		n++
	}
	// Collect newest-first; the iterator yields oldest-first (FIFO order).
	nodes := make([]*deltaKV, 0, n)
	for d := raw.added; d != nil; d = d.prev {
		nodes = append(nodes, d)
	}
	return raw.md, func(yield func(string, string) bool) {
		for i := len(nodes) - 1; i >= 0; i-- {
			kv := nodes[i].kv
			for j := 0; j+1 < len(kv); j += 2 {
				if !yield(kv[j], kv[j+1]) {
					return
				}
			}
		}
	}, true
}

// FromOutgoingContext returns the outgoing metadata in ctx if it exists.
//
// All keys in the returned MD are lowercase.
func FromOutgoingContext(ctx context.Context) (MD, bool) {
	raw, ok := ctx.Value(mdOutgoingKey{}).(rawMD)
	if !ok {
		return nil, false
	}

	mdSize := len(raw.md)
	for d := raw.added; d != nil; d = d.prev {
		mdSize += len(d.kv) / 2
	}

	out := make(MD, mdSize)
	for k, v := range raw.md {
		// We need to manually convert all keys to lower case, because MD is a
		// map, and there's no guarantee that the MD attached to the context is
		// created using our helper functions.
		key := strings.ToLower(k)
		out[key] = copyOf(v)
	}
	// Merge appended kv pairs in FIFO order. Collect nodes newest-first,
	// then replay oldest-first so later appends override earlier ones.
	n := 0
	for d := raw.added; d != nil; d = d.prev {
		n++
	}
	nodes := make([]*deltaKV, 0, n)
	for d := raw.added; d != nil; d = d.prev {
		nodes = append(nodes, d)
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		kv := nodes[i].kv
		for j := 0; j < len(kv); j += 2 {
			out[kv[j]] = append(out[kv[j]], kv[j+1])
		}
	}
	return out, ok
}

// deltaKV is a node in a singly-linked list of key-value slices appended
// via AppendToOutgoingContext.  The list is newest-first: each node's prev
// field points to the older delta.  Keys in kv are already lowercased.
type deltaKV struct {
	kv   []string
	prev *deltaKV
}

type rawMD struct {
	md    MD
	added *deltaKV
}
