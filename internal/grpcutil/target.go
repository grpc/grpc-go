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

package grpcutil

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc/resolver"
)

// ValidateTargetURI reports whether target is a valid gRPC dial target.
//
// A target is valid if:
//   - it parses as an RFC 3986 URI (via net/url.Parse), and
//   - its scheme has a resolver builder registered in the global registry
//     (resolver.Get).
//
// Validation is strict: a target without a scheme, or with a scheme that has
// no registered resolver, returns an error. No default-scheme fallback is
// applied, unlike grpc.NewClient. This is intended for validating targets
// received via configuration (e.g. xDS bootstrap server_uri) before dial
// time.
//
// Per-channel resolvers registered via grpc.WithResolvers are not visible to
// this function.
func ValidateTargetURI(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("grpcutil: invalid target URI %q: %v", target, err)
	}
	if resolver.Get(u.Scheme) == nil {
		return fmt.Errorf("grpcutil: target URI %q uses scheme %q which has no registered resolver", target, u.Scheme)
	}
	return nil
}
