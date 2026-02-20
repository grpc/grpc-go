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
// Hierarchical URIs (scheme://authority/path) with a non-empty scheme are
// validated directly: if builder returns nil for the scheme an error is
// returned immediately with no fallback.
//
// For opaque URIs (e.g. "host:port" where url.URL.Opaque is non-empty),
// empty-scheme URIs, and parse failures, ParseTarget retries by prepending
// defaultScheme + ":///" if defaultScheme is non-empty.
//
// builder is a function that returns the resolver.Builder for a given scheme,
// or nil if no resolver is registered. Pass resolver.Get to use the global
// resolver registry, or a custom lookup function (e.g. cc.getResolver) to
// also consider resolvers registered via dial options.
func ParseTarget(target, defaultScheme string, builder func(string) resolver.Builder) (resolver.Target, error) {
	u, err := url.Parse(target)
	if err == nil && u.Scheme != "" {
		if builder(u.Scheme) != nil {
			// Recognised scheme (hierarchical or opaque form) — use as-is.
			return resolver.Target{URL: *u}, nil
		}
		if u.Opaque == "" {
			// Unregistered scheme in hierarchical URI form (scheme://...): the
			// caller explicitly chose this scheme; do not silently fall back.
			return resolver.Target{}, fmt.Errorf("no resolver registered for scheme %q in target %q", u.Scheme, target)
		}
		// Opaque URI (e.g. "host:port") with unregistered scheme: treat the
		// same as an empty-scheme URI and fall through to the retry below.
	}
	// Parse error, empty scheme, or opaque URI with unregistered scheme:
	// retry by prepending defaultScheme if one is provided.
	if defaultScheme != "" {
		if u2, err2 := url.Parse(defaultScheme + ":///" + target); err2 == nil && builder(u2.Scheme) != nil {
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
