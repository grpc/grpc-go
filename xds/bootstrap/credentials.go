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

package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/xds/bootstrap/jwtcreds"
	"google.golang.org/grpc/internal/xds/bootstrap/tlscreds"
	"google.golang.org/protobuf/proto"

	access_tokenpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/call_credentials/access_token/v3"
	google_defaultpb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/google_default/v3"
	insecurepb "github.com/envoyproxy/go-control-plane/envoy/extensions/grpc_service/channel_credentials/insecure/v3"
)

func init() {
	RegisterChannelCredentials(&insecureCredsBuilder{})
	RegisterChannelCredentials(&googleDefaultCredsBuilder{})
	RegisterChannelCredentials(&tlsCredsBuilder{})

	RegisterCallCredentials(&jwtCallCredsBuilder{})
	RegisterCallCredentials(&accessTokenCallCredsBuilder{})
}

// insecureCredsBuilder implements the `ChannelCredentials` interface defined in
// package `xds/bootstrap` and encapsulates an insecure credential.
type insecureCredsBuilder struct{}

func (i *insecureCredsBuilder) Build(json.RawMessage) (credentials.Bundle, func(), error) {
	return insecure.NewBundle(), func() {}, nil
}

func (i *insecureCredsBuilder) Name() string {
	return "insecure"
}

func (i *insecureCredsBuilder) TypeURL() string {
	return "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.insecure.v3.InsecureCredentials"
}

func (i *insecureCredsBuilder) NewProtoConfig() proto.Message {
	return &insecurepb.InsecureCredentials{}
}

func (i *insecureCredsBuilder) MarshalProtoConfig(proto.Message) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

// tlsCredsBuilder implements the `ChannelCredentials` interface defined in
// package `xds/bootstrap` and encapsulates a TLS credential.
type tlsCredsBuilder struct{}

func (t *tlsCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, func(), error) {
	return tlscreds.NewBundle(config)
}

func (t *tlsCredsBuilder) Name() string {
	return "tls"
}

// googleDefaultCredsBuilder implements the `ChannelCredentials` interface defined in
// package `xds/bootstrap` and encapsulates a Google Default credential.
type googleDefaultCredsBuilder struct{}

func (d *googleDefaultCredsBuilder) Build(json.RawMessage) (credentials.Bundle, func(), error) {
	return google.NewDefaultCredentials(), func() {}, nil
}

func (d *googleDefaultCredsBuilder) Name() string {
	return "google_default"
}

func (d *googleDefaultCredsBuilder) TypeURL() string {
	return "type.googleapis.com/envoy.extensions.grpc_service.channel_credentials.google_default.v3.GoogleDefaultCredentials"
}

func (d *googleDefaultCredsBuilder) NewProtoConfig() proto.Message {
	return &google_defaultpb.GoogleDefaultCredentials{}
}

func (d *googleDefaultCredsBuilder) MarshalProtoConfig(proto.Message) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

// jwtCallCredsBuilder implements the `CallCredentials` interface defined in
// package `xds/bootstrap` and encapsulates JWT call credentials.
type jwtCallCredsBuilder struct{}

func (j *jwtCallCredsBuilder) Build(configJSON json.RawMessage) (credentials.PerRPCCredentials, func(), error) {
	return jwtcreds.NewCallCredentials(configJSON)
}

func (j *jwtCallCredsBuilder) Name() string {
	return "jwt_token_file"
}

// accessTokenCallCredsBuilder implements CallCredentials and
// CallCredentialsWithProto.
type accessTokenCallCredsBuilder struct{}

func (a *accessTokenCallCredsBuilder) Build(config json.RawMessage) (credentials.PerRPCCredentials, func(), error) {
	var cfg struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, nil, err
	}
	if cfg.Token == "" {
		return nil, nil, fmt.Errorf("access token must be non-empty")
	}
	return accessTokenCreds{token: cfg.Token}, func() {}, nil
}

func (a *accessTokenCallCredsBuilder) Name() string {
	return "access_token"
}

func (a *accessTokenCallCredsBuilder) TypeURL() string {
	return "type.googleapis.com/envoy.extensions.grpc_service.call_credentials.access_token.v3.AccessTokenCredentials"
}

func (a *accessTokenCallCredsBuilder) NewProtoConfig() proto.Message {
	return &access_tokenpb.AccessTokenCredentials{}
}

func (a *accessTokenCallCredsBuilder) MarshalProtoConfig(config proto.Message) (json.RawMessage, error) {
	msg, ok := config.(*access_tokenpb.AccessTokenCredentials)
	if !ok {
		return nil, fmt.Errorf("unexpected config type: %T", config)
	}
	if msg.GetToken() == "" {
		return nil, fmt.Errorf("access token must be non-empty")
	}
	cfgJSON, err := json.Marshal(map[string]string{"token": msg.GetToken()})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(cfgJSON), nil
}

type accessTokenCreds struct {
	token string
}

func (a accessTokenCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + a.token,
	}, nil
}

func (a accessTokenCreds) RequireTransportSecurity() bool {
	return true
}
