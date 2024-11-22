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
	"google.golang.org/grpc/xds"
)

// Config contains xDS fields applicable to LRS client.
// Config can be extended with more attributes in future.
type Config struct {
	Node             xds.Node             // Required
	TransportBuilder xds.TransportBuilder // Required

	Extensions any
}

// NewConfig returns an LRS config configured with mandatory parameters.
func NewConfig(node xds.Node, transport xds.TransportBuilder) (*Config, error) {
	return &Config{Node: node, TransportBuilder: transport}, nil
}
