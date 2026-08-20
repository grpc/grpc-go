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
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	xdscredspb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/xds/v3"
)

const xdsCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.xds.v3.XdsCredentials"

func init() {
	RegisterChannelCredsBuilder(xdsCredsTypeURL, xdsCredsBuilder{})
}

// xdsCredsBuilder builds channel credentials from an XdsCredentials plugin
// config. A side-channel target is not an xDS cluster, so there is no xDS
// security configuration for it; the xds credential therefore resolves to its
// required fallback credential, whose builder is looked up in the registry.
type xdsCredsBuilder struct{}

func (xdsCredsBuilder) Build(config *anypb.Any, bc *bootstrap.Config) (credentials.Bundle, func(), error) {
	var xdsCfg xdscredspb.XdsCredentials
	if err := anypb.UnmarshalTo(config, &xdsCfg, proto.UnmarshalOptions{}); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal XdsCredentials: %v", err)
	}
	fallback := xdsCfg.GetFallbackCredentials()
	if fallback == nil {
		return nil, nil, fmt.Errorf("xds credentials missing required fallback credentials")
	}
	b := GetChannelCredsBuilder(fallback.GetTypeUrl())
	if b == nil {
		return nil, nil, fmt.Errorf("unsupported fallback credentials type %q in xds credentials", fallback.GetTypeUrl())
	}
	return b.Build(fallback, bc)
}
