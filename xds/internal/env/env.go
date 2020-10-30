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

// Package env acts a single source of definition for all environment variables
// related to the xDS implementation in gRPC.
package env

const (
	// BootstrapFileName is the name of the environment variable which holds the
	// name of the bootstrap file.
	BootstrapFileName = "GRPC_XDS_BOOTSTRAP"
	// XDSV3Support is the name of the environment variable which indicates
	// whether or not xDS v3 transport protocol is supported. A value of "true"
	// indicates that the v3 protocol is supported, while all other values
	// indicate that the protocol is unsupported.
	XDSV3Support = "GRPC_XDS_EXPERIMENTAL_V3_SUPPORT"
)
