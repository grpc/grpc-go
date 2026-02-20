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

// ValidateTargetURI validates that target is a valid RFC 3986 URI
// and that a resolver is registered for its scheme.
func ValidateTargetURI(target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid target URI %q: %w", target, err)
	}

	if u.Scheme == "" {
		return fmt.Errorf("target URI %q has no scheme", target)
	}

	if resolver.Get(u.Scheme) == nil {
		return fmt.Errorf("no resolver registered for scheme %q", u.Scheme)
	}

	return nil
}
