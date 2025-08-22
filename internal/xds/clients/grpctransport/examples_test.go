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

package grpctransport_test

import (
	"fmt"

	"google.golang.org/grpc/internal/xds/clients"
	"google.golang.org/grpc/internal/xds/clients/grpctransport"
)

// ExampleServerIdentifierExtension demonstrates how to create
// clients.ServerIdentifier with grpctransport.ServerIdentifierExtension as
// its extensions.
//
// This example is creating clients.ServerIdentifier to connect to server at
// localhost:5678 using the config named "local". Note that "local" must
// exist as an entry in the provided configs to grpctransport.Builder.
func ExampleServerIdentifierExtension() {
	// Note the Extensions field is set by value and not by pointer.
	fmt.Printf("%+v", clients.ServerIdentifier{ServerURI: "localhost:5678", Extensions: grpctransport.ServerIdentifierExtension{ConfigName: "local"}})
	// Output: {ServerURI:localhost:5678 Extensions:{ConfigName:local}}
}
