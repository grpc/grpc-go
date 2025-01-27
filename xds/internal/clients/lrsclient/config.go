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

package lrsclient

import (
	"google.golang.org/grpc/xds/internal/clients"
)

// Config provides parameters for configuring the LRS client.
type Config struct {
	// Node is the identity of the client application, reporting load to the
	// xDS management server.
	Node clients.Node

	// TransportBuilder is the implementation to create a communication channel
	// to an xDS management server.
	TransportBuilder clients.TransportBuilder
}
