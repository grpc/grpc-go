/*
 *
 * Copyright 2022 gRPC authors.
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

package transport_test

import (
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
)

// TestNew covers that New() returns an error if the input *ServerConfig
// contains invalid content.
func (s) TestNew(t *testing.T) {
	tests := []struct {
		name       string
		opts       transport.Options
		wantErrStr string
	}{
		{
			name:       "missing server URI",
			opts:       transport.Options{ServerCfg: bootstrap.ServerConfig{}},
			wantErrStr: "missing server URI when creating a new transport",
		},
		{
			name:       "missing credentials",
			opts:       transport.Options{ServerCfg: bootstrap.ServerConfig{ServerURI: "server-address"}},
			wantErrStr: "missing credentials when creating a new transport",
		},
		{
			name: "missing update handler",
			opts: transport.Options{ServerCfg: bootstrap.ServerConfig{
				ServerURI: "server-address",
				Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
				NodeProto: &v3corepb.Node{},
			}},
			wantErrStr: "missing update handler when creating a new transport",
		},
		{
			name: "missing stream error handler",
			opts: transport.Options{
				ServerCfg: bootstrap.ServerConfig{
					ServerURI: "server-address",
					Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
					NodeProto: &v3corepb.Node{},
				},
				UpdateHandler: func(transport.ResourceUpdate) error { return nil },
			},
			wantErrStr: "missing stream error handler when creating a new transport",
		},
		{
			name: "node proto version mismatch for v3",
			opts: transport.Options{
				ServerCfg: bootstrap.ServerConfig{
					ServerURI:    "server-address",
					Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
					NodeProto:    &v2corepb.Node{},
					TransportAPI: version.TransportV3,
				},
				UpdateHandler:      func(transport.ResourceUpdate) error { return nil },
				StreamErrorHandler: func(error) {},
			},
			wantErrStr: "unexpected type *core.Node for NodeProto, want *corev3.Node",
		},
		{
			name: "happy case",
			opts: transport.Options{
				ServerCfg: bootstrap.ServerConfig{
					ServerURI:    "server-address",
					Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
					NodeProto:    &v3corepb.Node{},
					TransportAPI: version.TransportV3,
				},
				UpdateHandler:      func(transport.ResourceUpdate) error { return nil },
				StreamErrorHandler: func(error) {},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := transport.New(test.opts)
			defer func() {
				if c != nil {
					c.Close()
				}
			}()
			if (err != nil) != (test.wantErrStr != "") {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.opts, err, test.wantErrStr)
			}
			if err != nil && !strings.Contains(err.Error(), test.wantErrStr) {
				t.Fatalf("New(%+v) = %v, wantErr: %v", test.opts, err, test.wantErrStr)
			}
		})
	}
}
