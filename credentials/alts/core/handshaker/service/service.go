/*
 *
 * Copyright 2018 gRPC authors.
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

// Package service manages connections between the VM application and the ALTS
// handshaker service.
package service

import (
	"flag"
	"sync"

	grpc "google.golang.org/grpc"
)

var (
	// hsServiceAddr specifies the default ALTS handshaker service address in
	// the hypervisor.
	hsServiceAddr = flag.String("handshaker_service_address", "metadata.google.internal:8080", "ALTS handshaker gRPC service address")
	// hsConn represents a connection to hypervisor handshaker service.
	hsConn *grpc.ClientConn
	mu     sync.Mutex
	// hsDialer will be reassigned in tests.
	hsDialer = grpc.Dial
)

type dialer func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

// Dial dials the handshake service in the hypervisor. If a connection has
// already been established, this function returns it. Otherwise, a new
// connection is created,
func Dial() (*grpc.ClientConn, error) {
	mu.Lock()
	defer mu.Unlock()

	if hsConn == nil {
		// Create a new connection to the handshaker service. Note that
		// this connection stays open until the application is closed.
		var err error
		hsConn, err = hsDialer(*hsServiceAddr, grpc.WithInsecure())
		if err != nil {
			return nil, err
		}
	}
	return hsConn, nil
}
