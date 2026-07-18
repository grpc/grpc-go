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

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/resolver"
)

// ValidateTargetURI reports whether target is a valid gRPC dial target. It is
// intended for validating targets received via configuration (e.g. xDS
// bootstrap server_uri, RLS lookup_service) before dial time.
//
// A target is valid if:
//   - it parses as an RFC 3986 authority-form URI (via net/url.Parse) whose
//     scheme has a resolver builder registered in the global registry
//     (resolver.Get), or
//   - it does not parse as an authority-form URI (e.g. a "host:port" string
//     such as "trafficdirector.googleapis.com:443", which parses as an opaque
//     URI, or a string that does not parse at all), but is accepted after
//     applying the default scheme, mirroring grpc.NewClient's fallback
//     behavior for schemeless targets.
//
// Unlike grpc.NewClient, an authority-form URI ("scheme://...") with a scheme
// that has no registered resolver is rejected instead of falling back to the
// default scheme, so that scheme typos in configuration surface as errors.
//
// Per-channel resolvers registered via grpc.WithResolvers are not visible to
// this function.
func ValidateTargetURI(target string) error {
	if target == "" {
		return fmt.Errorf("resolver: target URI cannot be empty")
	}
	// Mirror grpc.NewClient's choice of default scheme: "dns", unless the
	// user overrode it via resolver.SetDefaultScheme.
	defScheme := "dns"
	if internal.UserSetDefaultScheme {
		defScheme = resolver.GetDefaultScheme()
	}
	u, err := url.Parse(target)
	if err != nil || u.Opaque != "" {
		// Not an authority-form URI: treat it as a host:port shorthand and
		// apply the default scheme, as grpc.NewClient does.
		if u, err = url.Parse(defScheme + ":///" + target); err != nil {
			return fmt.Errorf("resolver: invalid target URI %q: %v", target, err)
		}
	}
	if u.Scheme == "" {
		u.Scheme = defScheme
	}
	if resolver.Get(u.Scheme) == nil {
		return fmt.Errorf("resolver: target URI %q uses scheme %q which has no registered resolver", target, u.Scheme)
	}
	return nil
}
