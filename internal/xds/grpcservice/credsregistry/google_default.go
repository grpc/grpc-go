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

package credsregistry

import (
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/protobuf/types/known/anypb"
)

const googleDefaultCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials"

func init() {
	RegisterChannelCredsBuilder(googleDefaultCredsTypeURL, googleDefaultCredsBuilder{})
}

// googleDefaultCredsBuilder builds Google Default channel credentials from a
// GoogleDefaultCredentials plugin config.
type googleDefaultCredsBuilder struct{}

func (googleDefaultCredsBuilder) Build(*anypb.Any, *bootstrap.Config) (credentials.Bundle, func(), error) {
	return google.NewDefaultCredentials(), func() {}, nil
}
