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

// Package proxyattributes contains functions for getting and setting proxy
// attributes like the CONNECT address and user info.
package proxyattributes

import (
	"net/url"

	"google.golang.org/grpc/resolver"
)

type keyType string

const userAndConnectAddrKey = keyType("grpc.resolver.delegatingresolver.userAndConnectAddr")

type attr struct {
	user *url.Userinfo
	addr string
}

// Populate returns a copy of addr with attributes containing the
// provided user and connect address, which are needed during the CONNECT
// handshake for a proxy connection.
func Populate(resAddr resolver.Address, user *url.Userinfo, addr string) resolver.Address {
	resAddr.Attributes = resAddr.Attributes.WithValue(userAndConnectAddrKey, attr{user: user, addr: addr})
	return resAddr
}

// ProxyConnectAddr returns the proxy connect address in resolver.Address, or nil
// if not present. The returned data should not be mutated.
func ProxyConnectAddr(addr resolver.Address) string {
	attribute := addr.Attributes.Value(userAndConnectAddrKey)
	if attribute != nil {
		return attribute.(attr).addr
	}
	return ""
}

// User returns the user info in the resolver.Address, or nil if not present.
// The returned data should not be mutated.
func User(addr resolver.Address) *url.Userinfo {
	attribute := addr.Attributes.Value(userAndConnectAddrKey)
	if attribute != nil {
		return attribute.(attr).user
	}
	return nil
}
