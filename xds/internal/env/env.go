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
	bootstrapFileNameEnv = "GRPC_XDS_BOOTSTRAP"
	xdsV3SupportEnv      = "GRPC_XDS_EXPERIMENTAL_V3_SUPPORT"
)

var (
	// BootstrapFileName holds the name of the file which contains xDS bootstrap
	// configuration. Users can specify the location of the bootstrap file by
	// setting the environment variable "GRPC_XDS_BOOSTRAP".
	BootstrapFileName = os.Getenv(bootstrapFileNameEnv)
	// V3Support indicates whether xDS v3 API support is enabled, which can be
	// done by setting the environment variable
	// "GRPC_XDS_EXPERIMENTAL_V3_SUPPORT" to "true".
	V3Support = strings.EqualFold(os.Getenv(xdsV3SupportEnv), "true")
)
