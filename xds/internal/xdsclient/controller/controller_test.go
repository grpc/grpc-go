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
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource/version"
)

const testXDSServer = "xds-server"

// noopUpdateHandler ignores all updates. It's to be used in tests where the
// updates don't matter. To avoid potential nil panic.
var noopUpdateHandler = &testUpdateReceiver{
	f: func(rType xdsresource.ResourceType, d map[string]interface{}, md xdsresource.UpdateMetadata) {},
}

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
				Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
				NodeProto: testutils.EmptyNodeProtoV2,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := New(test.config, noopUpdateHandler, nil, nil, nil) // Only testing the config, other inputs are left as nil.
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

func (s) TestNewWithGRPCDial(t *testing.T) {
	config := &bootstrap.ServerConfig{
		ServerURI: testXDSServer,
		Creds:     grpc.WithTransportCredentials(insecure.NewCredentials()),
		NodeProto: testutils.EmptyNodeProtoV2,
	}

	customDialerCalled := false
	customDialer := func(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
		customDialerCalled = true
		return grpc.Dial(target, opts...)
	}

	// Set the dialer and make sure it is called.
	SetGRPCDial(customDialer)
	c, err := New(config, noopUpdateHandler, nil, nil, nil)
	if err != nil {
		t.Fatalf("New(%+v) = %v, want no error", config, err)
	}
	if c != nil {
		c.Close()
	}

	if !customDialerCalled {
		t.Errorf("New(%+v) custom dialer called = false, want true", config)
	}
	customDialerCalled = false

	// Reset the dialer and make sure it is not called.
	SetGRPCDial(grpc.Dial)
	c, err = New(config, noopUpdateHandler, nil, nil, nil)
	defer func() {
		if c != nil {
			c.Close()
		}
	}()
	if err != nil {
		t.Fatalf("New(%+v) = %v, want no error", config, err)
	}

	if customDialerCalled {
		t.Errorf("New(%+v) interceptor called = true, want false", config)
	}
}
