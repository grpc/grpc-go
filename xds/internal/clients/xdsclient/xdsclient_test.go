/*
 *
 * Copyright 2025 gRPC authors.
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

package xdsclient

import (
	"testing"

	"google.golang.org/grpc/xds/internal/clients"
	"google.golang.org/grpc/xds/internal/clients/grpctransport"
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
)

func (s) TestXDSClient_New(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "empty node ID",
			config:  Config{},
			wantErr: "xdsclient: node ID is empty",
		},
		{
			name: "nil resource types",
			config: Config{
				Node: clients.Node{ID: "node-id"},
			},
			wantErr: "xdsclient: resource types map is nil",
		},
		{
			name: "nil transport builder",
			config: Config{
				Node:          clients.Node{ID: "node-id"},
				ResourceTypes: map[string]ResourceType{xdsresource.V3ListenerURL: listenerType},
			},
			wantErr: "xdsclient: transport builder is nil",
		},
		{
			name: "no servers or authorities",
			config: Config{
				Node:             clients.Node{ID: "node-id"},
				ResourceTypes:    map[string]ResourceType{xdsresource.V3ListenerURL: listenerType},
				TransportBuilder: grpctransport.NewBuilder(),
			},
			wantErr: "xdsclient: no servers or authorities specified",
		},
		{
			name: "success with servers",
			config: Config{
				Node:             clients.Node{ID: "node-id"},
				ResourceTypes:    map[string]ResourceType{xdsresource.V3ListenerURL: listenerType},
				TransportBuilder: grpctransport.NewBuilder(),
				Servers:          []ServerConfig{{ServerIdentifier: clients.ServerIdentifier{ServerURI: "dummy-server"}}},
			},
			wantErr: "",
		},
		{
			name: "success with authorities",
			config: Config{
				Node:             clients.Node{ID: "node-id"},
				ResourceTypes:    map[string]ResourceType{xdsresource.V3ListenerURL: listenerType},
				TransportBuilder: grpctransport.NewBuilder(),
				Authorities:      map[string]Authority{"authority-name": {}},
			},
			wantErr: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(tt.config)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("New(%+v) failed: %v", tt.config, err)
				}
			} else {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("New(%+v) returned error %v, want error %q", tt.config, err, tt.wantErr)
				}
			}
			if c != nil {
				c.Close()
			}
		})
	}
}

func (s) TestXDSClient_Close(t *testing.T) {
	config := Config{
		Node:             clients.Node{ID: "node-id"},
		ResourceTypes:    map[string]ResourceType{xdsresource.V3ListenerURL: listenerType},
		TransportBuilder: grpctransport.NewBuilder(),
		Servers:          []ServerConfig{{ServerIdentifier: clients.ServerIdentifier{ServerURI: "dummy-server"}}},
	}
	c, err := New(config)
	if err != nil {
		t.Fatalf("New(%+v) failed: %v", config, err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("c.Close() failed: %v", err)
	}
	// Calling close again should not panic.
	if err := c.Close(); err != nil {
		t.Fatalf("c.Close() failed: %v", err)
	}
}
