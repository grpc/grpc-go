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

package hostname_test

import (
	"testing"

	"google.golang.org/grpc/balancer/hostname"
	"google.golang.org/grpc/resolver"
)

func TestHostname_SetAndGet(t *testing.T) {
	ep := resolver.Endpoint{}
	if h := hostname.Hostname(ep); h != "" {
		t.Errorf("empty = %q", h)
	}

	ep2 := hostname.Set(ep, "myservice.example.com")
	if h := hostname.Hostname(ep2); h != "myservice.example.com" {
		t.Errorf("got %q", h)
	}

	// empty hostname returns same endpoint
	ep3 := hostname.Set(ep2, "")
	if hostname.Hostname(ep3) != "myservice.example.com" {
		t.Error("empty should not overwrite")
	}
}
