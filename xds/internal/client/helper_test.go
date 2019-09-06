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
	"google.golang.org/grpc/xds/internal"
	basepb "google.golang.org/grpc/xds/internal/proto/envoy/api/v2/core/base"
)

const (
	balancerName = "foo-balancer"
	serviceName  = "foo-service"
)

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
		"badApiConfigSourceProto": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
			    "api_type": "GRPC",
				"badField": "foobar",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					}
				]
			}
		}`,
		"badTopLevelFieldInFile": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
			    "api_type": "GRPC",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					}
				]
			},
			"badField": "foobar"
		}`,
		"emptyNodeProto": `
		{
			"xds_server" : {
			    "api_type": "GRPC",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					}
				]
			}
		}`,
		"emptyApiConfigSourceProto": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			}
		}`,
		"badApiTypeInFile": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
			    "api_type": "REST",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					}
				]
			}
		}`,
		"noGrpcServices": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
			    "api_type": "GRPC",
			}
		}`,
		"tooManyGrpcServices": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_server" : {
			    "api_type": "GRPC",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					},
					{
						"google_grpc": {
							"target_uri": "foobar.googleapis.com:443"
						}
					}
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
			    "api_type": "GRPC",
			    "grpc_services": [
					{
						"google_grpc": {
							"target_uri": "trafficdirector.googleapis.com:443"
						}
					}
				]
			}
		}`,
	}
	insecureDefaults := &ConfigDefaults{
		BalancerName: balancerName,
		ServiceName:  serviceName,
	}
	defaultNodeProto := &basepb.Node{
		Metadata: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				internal.GrpcHostname: {
					Kind: &structpb.Value_StringValue{StringValue: serviceName},
				},
			},
		},
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
	}()

	tests := []struct {
		name             string
		fName            string
		defaults         *ConfigDefaults
		wantBalancerName string
		wantNodeProto    *basepb.Node
		// TODO: It doesn't look like there is an easy way to compare the value
		// stored in Creds with an expected value. Figure out a way to make it
		// testable.
	}{
		{
			name:             "non-existent-bootstrap-file",
			fName:            "dummy",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "bad-json-in-file",
			fName:            "badJSON",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "bad-nodeProto-in-file",
			fName:            "badNodeProto",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "bad-ApiConfigSourceProto-in-file",
			fName:            "badApiConfigSourceProto",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "bad-top-level-field-in-file",
			fName:            "badTopLevelFieldInFile",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "empty-nodeProto-in-file",
			fName:            "emptyNodeProto",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "empty-apiConfigSourceProto-in-file",
			fName:            "emptyApiConfigSourceProto",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "bad-api-type-in-file",
			fName:            "badApiTypeInFile",
			defaults:         insecureDefaults,
			wantBalancerName: balancerName,
			wantNodeProto:    defaultNodeProto,
		},
		{
			name:             "good-bootstrap",
			fName:            "goodBootstrap",
			defaults:         insecureDefaults,
			wantBalancerName: "trafficdirector.googleapis.com:443",
			wantNodeProto: &basepb.Node{
				Id: "ENVOY_NODE_ID",
				Metadata: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
							Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		cHelper := NewConfig(test.fName, test.defaults)
		if got := cHelper.BalancerName; got != test.wantBalancerName {
			t.Errorf("%s: cHelper.BalancerName is %s, want %s", test.name, got, test.wantBalancerName)
		}
		if got := cHelper.NodeProto; !proto.Equal(got, test.wantNodeProto) {
			t.Errorf("%s: cHelper.NodeProto is %#v, want %#v", test.name, got, test.wantNodeProto)
		}
	}
}
