/*
 *
 * Copyright 2024 gRPC authors.
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

package proxyattributes

import (
	"net/url"
	"testing"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/resolver"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestProxyConnectAddr tests ProxyConnectAddr returns the coorect connect
// address in the attribute.
func (s) TestProxyConnectAddr(t *testing.T) {
	addr := resolver.Address{
		Addr:       "test-address",
		Attributes: attributes.New(userAndConnectAddrKey, attr{user: nil, addr: "proxy-address"}),
	}

	// Validate ProxyConnectAddr returns empty string for missing attributes
	if got, want := ProxyConnectAddr(addr), "proxy-address"; got != want {
		t.Errorf("Unexpected ConnectAddr proxy atrribute = %v, want : %v", got, want)
	}
}

// TestUser tests User returns the correct user in the attribute.
func (s) TestUser(t *testing.T) {
	user := url.UserPassword("username", "password")
	addr := resolver.Address{
		Addr:       "test-address",
		Attributes: attributes.New(userAndConnectAddrKey, attr{user: user, addr: ""}),
	}

	// Validate User returns nil for missing attributes
	if got, want := User(addr), user; got != want {
		t.Errorf("unexpected User proxy attribute = %v, want %v", got, want)
	}
}

// TestEmptyProxyAttribute tests ProxyConnectAddr and User return empty string
// and nil respectively when not set.
func (s) TestEmptyProxyAttribute(t *testing.T) {
	addr := resolver.Address{
		Addr: "test-address",
	}

	// Validate ProxyConnectAddr returns empty string for missing attributes
	if got := ProxyConnectAddr(addr); got != "" {
		t.Errorf("Unexpected ConnectAddr proxy atrribute = %v, want empty string", got)
	}
	// Validate User returns nil for missing attributes
	if got := User(addr); got != nil {
		t.Errorf("unexpected User proxy attribute = %v, want nil", got)
	}
}

// TestPopulate tests Populate returns a copy of addr with attributes
// containing correct user and connect address.
func (s) TestPopulate(t *testing.T) {
	addr := resolver.Address{
		Addr: "test-address",
	}
	user := url.UserPassword("username", "password")
	connectAddr := "proxy-address"

	// Call Populate and validate attributes
	populatedAddr := Populate(addr, user, connectAddr)

	// Verify that the returned address is updated correctly
	if got, want := ProxyConnectAddr(populatedAddr), connectAddr; got != want {
		t.Errorf("Unexpected ConnectAddr proxy atrribute = %v, want %v", got, want)
	}

	if got, want := User(populatedAddr), user; got != want {
		t.Errorf("unexpected User proxy attribute = %v, want %v", got, want)
	}
}
