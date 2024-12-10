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

const proxyOptionsKey = keyType("grpc.resolver.delegatingresolver.proxyOptions")

// Options holds the proxy connection details needed during the CONNECT
// handshake. It includes the user information and the connect address.
type Options struct {
	User        *url.Userinfo
	ConnectAddr string
}

// Populate returns a copy of addr with attributes containing the provided user
// and connect address, which are needed during the CONNECT handshake for a
// proxy connection.
func Populate(addr resolver.Address, opts Options) resolver.Address {
	addr.Attributes = addr.Attributes.WithValue(proxyOptionsKey, opts)
	return addr
}

// ConnectAddr returns the proxy CONNECT address in resolver.Address, or empty
// string if not present.
func ConnectAddr(addr resolver.Address) string {
	if a := addr.Attributes.Value(proxyOptionsKey); a != nil {
		return a.(Options).ConnectAddr
	}
	return ""
}

// User returns the user info in the resolver.Address, or nil if not present.
func User(addr resolver.Address) *url.Userinfo {
	if a := addr.Attributes.Value(proxyOptionsKey); a != nil {
		return a.(Options).User
	}
	return nil
}
