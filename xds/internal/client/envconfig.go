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

package client

import (
	"os"
	"strings"
)

// TODO: there are multiple env variables, GRPC_XDS_BOOTSTRAP and
// GRPC_XDS_EXPERIMENTAL_V3_SUPPORT, and this. Move all env variables into a
// separate package.
const routingEnabledConfigStr = "GRPC_XDS_EXPERIMENTAL_ROUTING"

// routing is enabled only if env variable is set to true. The default is false.
// We may flip the default later.
var routingEnabled = strings.EqualFold(os.Getenv(routingEnabledConfigStr), "true")
