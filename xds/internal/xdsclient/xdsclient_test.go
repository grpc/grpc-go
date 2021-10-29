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

package xdsclient_test

import (
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v2" // Register the v2 API client.
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const testXDSServer = "xds-server"

func (s) TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  *bootstrap.Config
		wantErr bool
	}{
		{
			name:    "empty-opts",
			config:  &bootstrap.Config{},
			wantErr: true,
		},
		{
			name: "empty-balancer-name",
			config: &bootstrap.Config{
				XDSServer: &bootstrap.ServerConfig{
					Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
					NodeProto: testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-dial-creds",
			config: &bootstrap.Config{
				XDSServer: &bootstrap.ServerConfig{
					ServerURI: testXDSServer,
					NodeProto: testutils.EmptyNodeProtoV2,
				},
			},
			wantErr: true,
		},
		{
			name: "empty-node-proto",
			config: &bootstrap.Config{
				XDSServer: &bootstrap.ServerConfig{
					ServerURI: testXDSServer,
					Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
				},
			},
			wantErr: true,
		},
		{
			name: "node-proto-version-mismatch",
			config: &bootstrap.Config{
				XDSServer: &bootstrap.ServerConfig{
					ServerURI:    testXDSServer,
					Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
					TransportAPI: version.TransportV2,
					NodeProto:    testutils.EmptyNodeProtoV3,
				},
			},
			wantErr: true,
		},
		// TODO(easwars): Add cases for v3 API client.
		{
			name: "happy-case",
			config: &bootstrap.Config{
				XDSServer: &bootstrap.ServerConfig{
					ServerURI: testXDSServer,
					Creds:     grpc.WithInsecure(),
					NodeProto: testutils.EmptyNodeProtoV2,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := xdsclient.NewWithConfigForTesting(test.config, 15*time.Second)
			if (err != nil) != test.wantErr {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.config, err, test.wantErr)
			}
			if c != nil {
				c.Close()
			}
		})
	}
}
