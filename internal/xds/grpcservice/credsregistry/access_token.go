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
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/xds/grpcservice/accesstokencreds"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	accesstokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
)

const accessTokenCredsTypeURL = "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials"

func init() {
	RegisterCallCredsBuilder(accessTokenCredsTypeURL, accessTokenCredsBuilder{})
}

// accessTokenCredsBuilder builds static access token call credentials from an
// AccessTokenCredentials plugin config.
type accessTokenCredsBuilder struct{}

func (accessTokenCredsBuilder) Build(config *anypb.Any) (credentials.PerRPCCredentials, func(), error) {
	var accessToken accesstokenpb.AccessTokenCredentials
	if err := anypb.UnmarshalTo(config, &accessToken, proto.UnmarshalOptions{}); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal AccessTokenCredentials: %v", err)
	}
	if accessToken.GetToken() == "" {
		return nil, nil, fmt.Errorf("access token must be non-empty")
	}
	cfgJSON, err := json.Marshal(map[string]string{"token": accessToken.GetToken()})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal access token config: %v", err)
	}
	cc, err := accesstokencreds.NewCallCredentials(cfgJSON)
	if err != nil {
		return nil, nil, err
	}
	// These credentials hold no resources; the no-op cleanup satisfies the
	// registry's Build contract.
	return cc, func() {}, nil
}
