/*
 *
 * Copyright 2020 gRPC authors.
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

package testutils

import "net"

// AvailableHostPort returns a local address to listen on. This will be of the
// form "host:port", where the host will be a literal IP address, and port
// must be a literal port number. If the host is a literal IPv6 address it
// will be enclosed in square brackets, as in "[2001:db8::1]:80.
//
// This is useful for tests which need to call the Serve() method on
// xds.GRPCServer which needs to be passed an IP:Port to listen on, where the IP
// must be a literal IP and not localhost. This approach will work on support
// one or both of IPv4 or IPv6.
func AvailableHostPort() (string, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", err
	}
	addr := l.Addr().String()
	l.Close()
	return addr, nil
}
