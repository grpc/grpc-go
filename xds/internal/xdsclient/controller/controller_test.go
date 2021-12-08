/*
 *
 * Copyright 2021 gRPC authors.
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
 */

package controller

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

const testXDSServer = "xds-server"

// TestNew covers that New() returns an error if the input *ServerConfig
// contains invalid content.
func (s) TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  *bootstrap.ServerConfig
		wantErr bool
	}{
		{
			name:    "empty-opts",
			config:  &bootstrap.ServerConfig{},
			wantErr: true,
		},
		{
			name: "empty-balancer-name",
			config: &bootstrap.ServerConfig{
				Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
				NodeProto: testutils.EmptyNodeProtoV2,
			},
			wantErr: true,
		},
		{
			name: "empty-dial-creds",
			config: &bootstrap.ServerConfig{
				ServerURI: testXDSServer,
				NodeProto: testutils.EmptyNodeProtoV2,
			},
			wantErr: true,
		},
		{
			name: "empty-node-proto",
			config: &bootstrap.ServerConfig{
				ServerURI: testXDSServer,
				Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
			},
			wantErr: true,
		},
		{
			name: "node-proto-version-mismatch",
			config: &bootstrap.ServerConfig{
				ServerURI:    testXDSServer,
				Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
				TransportAPI: version.TransportV2,
				NodeProto:    testutils.EmptyNodeProtoV3,
			},
			wantErr: true,
		},
		{
			name: "happy-case",
			config: &bootstrap.ServerConfig{
				ServerURI: testXDSServer,
				Creds:     grpc.WithInsecure(),
				NodeProto: testutils.EmptyNodeProtoV2,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := New(test.config, nil, nil, nil) // Only testing the config, other inputs are left as nil.
			defer func() {
				if c != nil {
					c.Close()
				}
			}()
			if (err != nil) != test.wantErr {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.config, err, test.wantErr)
			}
		})
	}
}
