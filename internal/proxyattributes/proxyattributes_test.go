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

// Tests that ConnectAddr returns the correct connect address in the attribute.
func (s) TestConnectAddr(t *testing.T) {
	tests := []struct {
		name string
		addr resolver.Address
		want string
	}{
		{
			name: "connect address in attribute",
			addr: resolver.Address{
				Addr: "test-address",
				Attributes: attributes.New(proxyOptionsKey, Options{
					ConnectAddr: "proxy-address",
				}),
			},
			want: "proxy-address",
		},
		{
			name: "no attribute",
			addr: resolver.Address{Addr: "test-address"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConnectAddr(tt.addr); got != tt.want {
				t.Errorf("ConnetAddr(%v) = %v, want %v ", tt.addr, got, tt.want)
			}
		})
	}
}

// Tests that User returns the correct user in the attribute.
func (s) TestUser(t *testing.T) {
	user := url.UserPassword("username", "password")
	tests := []struct {
		name string
		addr resolver.Address
		want *url.Userinfo
	}{
		{
			name: "user in attribute",
			addr: resolver.Address{
				Addr: "test-address",
				Attributes: attributes.New(proxyOptionsKey, Options{
					User: user,
				})},
			want: user,
		},
		{
			name: "no attribute",
			addr: resolver.Address{Addr: "test-address"},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := User(tt.addr); got != tt.want {
				t.Errorf("User(%v) = %v, want %v ", tt.addr, got, tt.want)
			}
		})
	}
}

// Tests that Populate returns a copy of addr with attributes containing correct
// user and connect address.
func (s) TestPopulate(t *testing.T) {
	addr := resolver.Address{Addr: "test-address"}
	pOpts := Options{
		User:        url.UserPassword("username", "password"),
		ConnectAddr: "proxy-address",
	}

	// Call Populate and validate attributes
	populatedAddr := Populate(addr, pOpts)

	if got, want := ConnectAddr(populatedAddr), pOpts.ConnectAddr; got != want {
		t.Errorf("Unexpected ConnectAddr proxy atrribute = %v, want %v", got, want)
	}

	if got, want := User(populatedAddr), pOpts.User; got != want {
		t.Errorf("unexpected User proxy attribute = %v, want %v", got, want)
	}
}
