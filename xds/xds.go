/*
 *
 * Copyright 2019 gRPC authors.
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

// Package xds implements a balancer that communicates with a remote balancer
// using the Envoy xDS protocol.
package xds

// Currently this package only imports the xds experimental and internal
// packages. Once the code is considered stable enough, we will move it out of
// experimental to here.
import (
	_ "google.golang.org/grpc/xds/experimental"
	_ "google.golang.org/grpc/xds/internal"
)
