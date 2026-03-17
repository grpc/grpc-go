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

// Tests that Get returns a valid proxy attribute.
func (s) TestGet(t *testing.T) {
	user := url.UserPassword("username", "password")
	tests := []struct {
		name            string
		addr            resolver.Address
		wantConnectAddr string
		wantUser        *url.Userinfo
		wantAttrPresent bool
	}{
		{
			name: "connect_address_in_attribute",
			addr: resolver.Address{
				Addr: "test-address",
				Attributes: attributes.New(proxyOptionsKey, Options{
					ConnectAddr: "proxy-address",
				}),
			},
			wantConnectAddr: "proxy-address",
			wantAttrPresent: true,
		},
		{
			name: "user_in_attribute",
			addr: resolver.Address{
				Addr: "test-address",
				Attributes: attributes.New(proxyOptionsKey, Options{
					User: user,
				}),
			},
			wantUser:        user,
			wantAttrPresent: true,
		},
		{
			name:            "no_attribute",
			addr:            resolver.Address{Addr: "test-address"},
			wantAttrPresent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOption, attrPresent := Get(tt.addr)
			if attrPresent != tt.wantAttrPresent {
				t.Errorf("Get(%v) = %v, want %v", tt.addr, attrPresent, tt.wantAttrPresent)
			}

			if gotOption.ConnectAddr != tt.wantConnectAddr {
				t.Errorf("ConnectAddr(%v) = %v, want %v", tt.addr, gotOption.ConnectAddr, tt.wantConnectAddr)
			}

			if gotOption.User != tt.wantUser {
				t.Errorf("User(%v) = %v, want %v", tt.addr, gotOption.User, tt.wantUser)
			}
		})
	}
}

// Tests that Set returns a copy of addr with attributes containing correct
// user and connect address.
func (s) TestSet(t *testing.T) {
	addr := resolver.Address{Addr: "test-address"}
	pOpts := Options{
		User:        url.UserPassword("username", "password"),
		ConnectAddr: "proxy-address",
	}

	// Call Set and validate attributes
	populatedAddr := Set(addr, pOpts)
	gotOption, attrPresent := Get(populatedAddr)
	if !attrPresent {
		t.Errorf("Get(%v) = %v, want %v ", populatedAddr, attrPresent, true)
	}
	if got, want := gotOption.ConnectAddr, pOpts.ConnectAddr; got != want {
		t.Errorf("unexpected ConnectAddr proxy atrribute = %v, want %v", got, want)
	}
	if got, want := gotOption.User, pOpts.User; got != want {
		t.Errorf("unexpected User proxy attribute = %v, want %v", got, want)
	}
}
