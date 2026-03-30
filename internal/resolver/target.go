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

package resolver

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc/resolver"
)

// ParseTarget parses a gRPC target string into a resolver.Target, verifying
// that a resolver is registered for the parsed scheme using builder.
//
// If the target parses successfully and builder recognises the scheme, the
// parsed target is returned directly. When the scheme is unregistered, empty,
// or parsing fails, ParseTarget retries by prepending defaultScheme + ":///"
// if defaultScheme is non-empty.
//
// builder is a function that returns the resolver.Builder for a given scheme,
// or nil if no resolver is registered. Pass resolver.Get to use the global
// resolver registry, or a custom lookup function (e.g. cc.getResolver) to
// also consider resolvers registered via dial options.
func ParseTarget(target, defaultScheme string, builder func(string) resolver.Builder) (resolver.Target, error) {
	u, err := url.Parse(target)
	if err == nil && u.Scheme != "" && builder(u.Scheme) != nil {
		return resolver.Target{URL: *u}, nil
	}
	// Parse error, empty scheme, or unregistered scheme: retry with the
	// default scheme if one is provided.
	if defaultScheme != "" && builder(defaultScheme) != nil {
		canonicalTarget := defaultScheme + ":///" + target
		if u2, err2 := url.Parse(canonicalTarget); err2 == nil {
			return resolver.Target{URL: *u2}, nil
		}
	}
	if err != nil {
		return resolver.Target{}, fmt.Errorf("invalid target URI %q: %v", target, err)
	}
	if u.Scheme == "" {
		return resolver.Target{}, fmt.Errorf("target URI %q has no scheme", target)
	}
	return resolver.Target{}, fmt.Errorf("no resolver registered for scheme %q in target %q", u.Scheme, target)
}
