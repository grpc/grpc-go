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

package client

import (
	"os"
	"testing"

	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	basepb "google.golang.org/grpc/xds/internal/proto/envoy/api/v2/core/base"
)

var (
	nodeProto = &basepb.Node{
		Id: "ENVOY_NODE_ID",
		Metadata: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
					Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
				},
			},
		},
	}
	nilCredsConfig = &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        nil,
		NodeProto:    nodeProto,
	}
	nonNilCredsConfig = &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
		NodeProto:    nodeProto,
	}
)

// TestNewConfig exercises the functionality in NewConfig with different
// bootstrap file contents. It overrides the fileReadFunc by returning
// bootstrap file contents defined in this test, instead of reading from a
// file.
func TestNewConfig(t *testing.T) {
	bootstrapFileMap := map[string]string{
		"empty":   "",
		"badJSON": `["test": 123]`,
		"badNodeProto": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"badField": "foobar",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			}
		}`,
		"badXdsServerConfig": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443"
			    "api_type": "GRPC",
				"badField": "foobar",
			}
		}`,
		"emptyNodeProto": `
		{
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443"
			}
		}`,
		"emptyXdsServer": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			}
		}`,
		"unknownTopLevelFieldInFile": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				]
			},
			"unknownField": "foobar"
		}`,
		"unknownFieldInXdsServer": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				],
				"unknownField": "foobar"
			}
		}`,
		"emptyChannelCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443"
			}
		}`,
		"nonGoogleDefaultCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				]
			}
		}`,
		"multipleChannelCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" },
					{ "type": "google_default" }
				]
			}
		}`,
		"goodBootstrap": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "google_default" }
				]
			}
		}`,
	}

	oldFileReadFunc := fileReadFunc
	fileReadFunc = func(name string) ([]byte, error) {
		if b, ok := bootstrapFileMap[name]; ok {
			return []byte(b), nil
		}
		return nil, os.ErrNotExist
	}
	defer func() {
		fileReadFunc = oldFileReadFunc
		os.Unsetenv(bootstrapFileEnv)
	}()

	tests := []struct {
		name       string
		fName      string
		wantConfig *Config
	}{
		{
			name:       "non-existent-bootstrap-file",
			fName:      "dummy",
			wantConfig: &Config{},
		},
		{
			name:       "empty-bootstrap-file",
			fName:      "empty",
			wantConfig: &Config{},
		},
		{
			name:       "bad-json-in-file",
			fName:      "badJSON",
			wantConfig: &Config{},
		},
		{
			name:       "bad-nodeProto-in-file",
			fName:      "badNodeProto",
			wantConfig: &Config{},
		},
		{
			name:       "bad-xds-server-config-in-file",
			fName:      "badXdsServerConfig",
			wantConfig: &Config{},
		},
		{
			name:       "empty-nodeProto-in-file",
			fName:      "emptyNodeProto",
			wantConfig: &Config{BalancerName: "trafficdirector.googleapis.com:443"},
		},
		{
			name:       "empty-xdsServer-in-file",
			fName:      "emptyXdsServer",
			wantConfig: &Config{NodeProto: nodeProto},
		},
		{
			name:       "unknown-top-level-field-in-file",
			fName:      "unknownTopLevelFieldInFile",
			wantConfig: nilCredsConfig,
		},
		{
			name:       "unknown-field-in-xds-server",
			fName:      "unknownFieldInXdsServer",
			wantConfig: nilCredsConfig,
		},
		{
			name:       "empty-channel-creds",
			fName:      "emptyChannelCreds",
			wantConfig: nilCredsConfig,
		},
		{
			name:       "non-google-default-creds",
			fName:      "nonGoogleDefaultCreds",
			wantConfig: nilCredsConfig,
		},
		{
			name:       "multiple-channel-creds",
			fName:      "multipleChannelCreds",
			wantConfig: nonNilCredsConfig,
		},
		{
			name:       "good-bootstrap",
			fName:      "goodBootstrap",
			wantConfig: nonNilCredsConfig,
		},
	}

	for _, test := range tests {
		if err := os.Setenv(bootstrapFileEnv, test.fName); err != nil {
			t.Fatalf("%s: os.Setenv(%s, %s) failed with error: %v", test.name, bootstrapFileEnv, test.fName, err)
		}
		config := NewConfig()
		if config.BalancerName != test.wantConfig.BalancerName {
			t.Errorf("%s: config.BalancerName is %s, want %s", test.name, config.BalancerName, test.wantConfig.BalancerName)
		}
		if !proto.Equal(config.NodeProto, test.wantConfig.NodeProto) {
			t.Errorf("%s: config.NodeProto is %#v, want %#v", test.name, config.NodeProto, test.wantConfig.NodeProto)
		}
		if (config.Creds != nil) != (test.wantConfig.Creds != nil) {
			t.Errorf("%s: config.Creds is %#v, want %#v", test.name, config.Creds, test.wantConfig.Creds)
		}
	}
}

func TestNewConfigEnvNotSet(t *testing.T) {
	os.Unsetenv(bootstrapFileEnv)
	wantConfig := Config{}
	if config := NewConfig(); *config != wantConfig {
		t.Errorf("NewConfig() returned : %#v, wanted an empty Config object", config)
	}
}
