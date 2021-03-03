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
 *
 */

// Package xds contains types that need to be shared between code under
// google.golang.org/grpc/xds/... and the rest of gRPC.
package xds

import "regexp"

// StringMatcher contains match criteria for matching a string, and is an
// internal representation of the `StringMatcher` proto defined at
// https://github.com/envoyproxy/envoy/blob/main/api/envoy/type/matcher/v3/string.proto.
type StringMatcher struct {
	// Since these match fields are part of a `oneof` in the corresponding xDS
	// proto, only one of them is expected to be set.
	ExactMatch    *string
	PrefixMatch   *string
	SuffixMatch   *string
	RegexMatch    *regexp.Regexp
	ContainsMatch *string
	// If true, indicates the exact/prefix/suffix/contains matching should be
	// case insensitive. This has no effect on the regex match.
	IgnoreCase bool
}
