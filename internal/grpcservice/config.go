/*
 *
 * Copyright 2026 gRPC authors.
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

// Package grpcservice provides shared utilities for converting, validating,
// and comparing xDS GrpcService configuration representations.
package grpcservice

import "time"

// ChannelCreds is a comparable representation of xDS Channel Credentials.
type ChannelCreds struct {
	Type   string
	Config string // JSON configuration as a raw string
}

// CallCreds defines a comparable representation of xDS Call Credentials.
type CallCreds struct {
	Type   string
	Config string // JSON configuration as a raw string
}

// Config is a comparable struct representing the parsed and
// resolved GrpcService configuration. Safe for use as a map key.
type Config struct {
	TargetURI          string
	ChannelCredentials ChannelCreds
	CallCredentials    string // JSON-serialized sorted list of CallCreds
	Timeout            time.Duration
	InitialMetadata    string // JSON-serialized sorted key-value pairs representing headers
}
