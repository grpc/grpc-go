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

package clients

// ServerConfig contains the configuration to connect to a server.
type ServerConfig struct {
	ServerURI string

	Extensions any
}

// Authority provides the functionality required to communicate with
// management servers corresponding to an authority.
type Authority struct {
	XDSServers []ServerConfig

	Extensions any
}

// Node is the representation of the client node of xDS Client.
type Node struct {
	ID               string
	Cluster          string
	Locality         Locality
	Metadata         any
	UserAgentName    string
	UserAgentVersion string
}

// Locality is the representation of the locality field within a node.
type Locality struct {
	Region  string
	Zone    string
	SubZone string
}
