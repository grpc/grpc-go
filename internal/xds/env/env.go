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

import (
	"os"
	"strings"
)

const (
	// BootstrapFileNameEnv is the env variable to set bootstrap file name.
	// Do not use this and read from env directly. Its value is read and kept in
	// variable BootstrapFileName.
	//
	// When both bootstrap FileName and FileContent are set, FileName is used.
	BootstrapFileNameEnv = "GRPC_XDS_BOOTSTRAP"
	// BootstrapFileContentEnv is the env variable to set bootstrapp file
	// content. Do not use this and read from env directly. Its value is read
	// and kept in variable BootstrapFileName.
	//
	// When both bootstrap FileName and FileContent are set, FileName is used.
	BootstrapFileContentEnv      = "GRPC_XDS_BOOTSTRAP_CONFIG"
	circuitBreakingSupportEnv    = "GRPC_XDS_EXPERIMENTAL_CIRCUIT_BREAKING"
	timeoutSupportEnv            = "GRPC_XDS_EXPERIMENTAL_ENABLE_TIMEOUT"
	faultInjectionSupportEnv     = "GRPC_XDS_EXPERIMENTAL_FAULT_INJECTION"
	clientSideSecuritySupportEnv = "GRPC_XDS_EXPERIMENTAL_SECURITY_SUPPORT"

	c2pResolverSupportEnv                    = "GRPC_EXPERIMENTAL_GOOGLE_C2P_RESOLVER"
	c2pResolverTestOnlyTrafficDirectorURIEnv = "GRPC_TEST_ONLY_GOOGLE_C2P_RESOLVER_TRAFFIC_DIRECTOR_URI"
)

var (
	// BootstrapFileName holds the name of the file which contains xDS bootstrap
	// configuration. Users can specify the location of the bootstrap file by
	// setting the environment variable "GRPC_XDS_BOOSTRAP".
	//
	// When both bootstrap FileName and FileContent are set, FileName is used.
	BootstrapFileName = os.Getenv(BootstrapFileNameEnv)
	// BootstrapFileContent holds the content of the xDS bootstrap
	// configuration. Users can specify the bootstrap config by
	// setting the environment variable "GRPC_XDS_BOOSTRAP_CONFIG".
	//
	// When both bootstrap FileName and FileContent are set, FileName is used.
	BootstrapFileContent = os.Getenv(BootstrapFileContentEnv)
	// CircuitBreakingSupport indicates whether circuit breaking support is
	// enabled, which can be disabled by setting the environment variable
	// "GRPC_XDS_EXPERIMENTAL_CIRCUIT_BREAKING" to "false".
	CircuitBreakingSupport = !strings.EqualFold(os.Getenv(circuitBreakingSupportEnv), "false")
	// TimeoutSupport indicates whether support for max_stream_duration in
	// route actions is enabled.  This can be disabled by setting the
	// environment variable "GRPC_XDS_EXPERIMENTAL_ENABLE_TIMEOUT" to "false".
	TimeoutSupport = !strings.EqualFold(os.Getenv(timeoutSupportEnv), "false")
	// FaultInjectionSupport is used to control both fault injection and HTTP
	// filter support.
	FaultInjectionSupport = !strings.EqualFold(os.Getenv(faultInjectionSupportEnv), "false")
	// C2PResolverSupport indicates whether support for C2P resolver is enabled.
	// This can be enabled by setting the environment variable
	// "GRPC_EXPERIMENTAL_GOOGLE_C2P_RESOLVER" to "true".
	C2PResolverSupport = strings.EqualFold(os.Getenv(c2pResolverSupportEnv), "true")
	// ClientSideSecuritySupport is used to control processing of security
	// configuration on the client-side.
	//
	// Note that there is no env var protection for the server-side because we
	// have a brand new API on the server-side and users explicitly need to use
	// the new API to get security integration on the server.
	ClientSideSecuritySupport = strings.EqualFold(os.Getenv(clientSideSecuritySupportEnv), "true")
	// C2PResolverTestOnlyTrafficDirectorURI is the TD URI for testing.
	C2PResolverTestOnlyTrafficDirectorURI = os.Getenv(c2pResolverTestOnlyTrafficDirectorURIEnv)
)
