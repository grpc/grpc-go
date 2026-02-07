/*
 *
 * Copyright 2025 gRPC authors.
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

package grpcutil

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc/resolver"
)

// ParseTarget parses a gRPC target string into a URL and verifies that a
// resolver is registered for the parsed scheme. If parsing fails and
// defaultScheme is provided, it prepends defaultScheme and retries. If
// the scheme is empty after parsing, it is set to defaultScheme. Returns
// an error if no resolver is registered for the resulting scheme.
func ParseTarget(target, defaultScheme string) (*url.URL, error) {
	u, err := url.Parse(target)
	if err != nil {
		if defaultScheme == "" {
			return nil, fmt.Errorf("invalid target URI %q: %w", target, err)
		}
		u, err = url.Parse(defaultScheme + ":///" + target)
		if err != nil {
			return nil, fmt.Errorf("invalid target URI %q: %w", target, err)
		}
	}
	if u.Scheme == "" {
		if defaultScheme == "" {
			return nil, fmt.Errorf("target URI %q has no scheme", target)
		}
		u.Scheme = defaultScheme
	}
	if resolver.Get(u.Scheme) == nil {
		return nil, fmt.Errorf("no resolver registered for scheme %q in target %q", u.Scheme, target)
	}
	return u, nil
}
