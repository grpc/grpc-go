// +build !appengine

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

package bootstrap

import (
	"fmt"
	"os"
	"testing"

	v2corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/proto"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/xds/internal/version"
)

var (
	v2BootstrapFileMap = map[string]string{
		"emptyNodeProto": `
		{
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443"
			}]
		}`,
		"unknownTopLevelFieldInFile": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				]
			}],
			"unknownField": "foobar"
		}`,
		"unknownFieldInNodeProto": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"unknownField": "foobar",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443"
			}]
		}`,
		"unknownFieldInXdsServer": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				],
				"unknownField": "foobar"
			}]
		}`,
		"emptyChannelCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443"
			}]
		}`,
		"nonGoogleDefaultCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" }
				]
			}]
		}`,
		"multipleChannelCreds": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "not-google-default" },
					{ "type": "google_default" }
				]
			}]
		}`,
		"goodBootstrap": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "google_default" }
				]
			}]
		}`,
		"multipleXDSServers": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [
				{
					"server_uri": "trafficdirector.googleapis.com:443",
					"channel_creds": [{ "type": "google_default" }]
				},
				{
					"server_uri": "backup.never.use.com:1234",
					"channel_creds": [{ "type": "not-google-default" }]
				}
			]
		}`,
	}
	v3BootstrapFileMap = map[string]string{
		"serverDoesNotSupportsV3": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "google_default" }
				]
			}],
			"server_features" : ["foo", "bar"]
		}`,
		"serverSupportsV3": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "google_default" }
				]
			}],
			"server_features" : ["foo", "bar", "xds_v3"]
		}`,
	}
	metadata = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
				Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
			},
		},
	}
	v2NodeProto = &v2corepb.Node{
		Id:                   "ENVOY_NODE_ID",
		Metadata:             metadata,
		BuildVersion:         gRPCVersion,
		UserAgentName:        gRPCUserAgentName,
		UserAgentVersionType: &v2corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
		ClientFeatures:       []string{clientFeatureNoOverprovisioning},
	}
	v3NodeProto = &v3corepb.Node{
		Id:                   "ENVOY_NODE_ID",
		Metadata:             metadata,
		UserAgentName:        gRPCUserAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
		ClientFeatures:       []string{clientFeatureNoOverprovisioning},
	}
	nilCredsConfigV2 = &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        nil,
		NodeProto:    v2NodeProto,
	}
	nonNilCredsConfigV2 = &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
		NodeProto:    v2NodeProto,
	}
	nonNilCredsConfigV3 = &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
		TransportAPI: version.TransportV3,
		NodeProto:    v3NodeProto,
	}
)

func (c *Config) compare(want *Config) error {
	if c.BalancerName != want.BalancerName {
		return fmt.Errorf("config.BalancerName is %s, want %s", c.BalancerName, want.BalancerName)
	}
	// Since Creds is of type grpc.DialOption interface, where the
	// implementation is provided by a function, it is not possible to compare.
	if (c.Creds != nil) != (want.Creds != nil) {
		return fmt.Errorf("config.Creds is %#v, want %#v", c.Creds, want.Creds)
	}
	if c.TransportAPI != want.TransportAPI {
		return fmt.Errorf("config.TransportAPI is %v, want %v", c.TransportAPI, want.TransportAPI)

	}
	if diff := cmp.Diff(want.NodeProto, c.NodeProto, cmp.Comparer(proto.Equal)); diff != "" {
		return fmt.Errorf("config.NodeProto diff (-want, +got):\n%s", diff)
	}
	return nil
}

func setupBootstrapOverride(bootstrapFileMap map[string]string) func() {
	oldFileReadFunc := bootstrapFileReadFunc
	bootstrapFileReadFunc = func(name string) ([]byte, error) {
		if b, ok := bootstrapFileMap[name]; ok {
			return []byte(b), nil
		}
		return nil, os.ErrNotExist
	}
	return func() {
		bootstrapFileReadFunc = oldFileReadFunc
		os.Unsetenv(bootstrapFileEnv)
	}
}

// TODO: enable leak check for this package when
// https://github.com/googleapis/google-cloud-go/issues/2417 is fixed.

// TestNewConfigV2ProtoFailure exercises the functionality in NewConfig with
// different bootstrap file contents which are expected to fail.
func TestNewConfigV2ProtoFailure(t *testing.T) {
	bootstrapFileMap := map[string]string{
		"empty":          "",
		"badJSON":        `["test": 123]`,
		"noBalancerName": `{"node": {"id": "ENVOY_NODE_ID"}}`,
		"emptyXdsServer": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			}
		}`,
	}
	cancel := setupBootstrapOverride(bootstrapFileMap)
	defer cancel()

	tests := []struct {
		name      string
		wantError bool
	}{
		{"nonExistentBootstrapFile", true},
		{"empty", true},
		{"badJSON", true},
		{"noBalancerName", true},
		{"emptyXdsServer", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Setenv(bootstrapFileEnv, test.name); err != nil {
				t.Fatalf("os.Setenv(%s, %s) failed with error: %v", bootstrapFileEnv, test.name, err)
			}
			if _, err := NewConfig(); err == nil {
				t.Fatalf("NewConfig() returned nil error, expected to fail")
			}
		})
	}
}

// TestNewConfigV2ProtoSuccess exercises the functionality in NewConfig with
// different bootstrap file contents. It overrides the fileReadFunc by returning
// bootstrap file contents defined in this test, instead of reading from a file.
func TestNewConfigV2ProtoSuccess(t *testing.T) {
	cancel := setupBootstrapOverride(v2BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{
			"emptyNodeProto", &Config{
				BalancerName: "trafficdirector.googleapis.com:443",
				NodeProto: &v2corepb.Node{
					BuildVersion:         gRPCVersion,
					UserAgentName:        gRPCUserAgentName,
					UserAgentVersionType: &v2corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
					ClientFeatures:       []string{clientFeatureNoOverprovisioning},
				},
			},
		},
		{"unknownTopLevelFieldInFile", nilCredsConfigV2},
		{"unknownFieldInNodeProto", nilCredsConfigV2},
		{"unknownFieldInXdsServer", nilCredsConfigV2},
		{"emptyChannelCreds", nilCredsConfigV2},
		{"nonGoogleDefaultCreds", nilCredsConfigV2},
		{"multipleChannelCreds", nonNilCredsConfigV2},
		{"goodBootstrap", nonNilCredsConfigV2},
		{"multipleXDSServers", nonNilCredsConfigV2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Setenv(bootstrapFileEnv, test.name); err != nil {
				t.Fatalf("os.Setenv(%s, %s) failed with error: %v", bootstrapFileEnv, test.name, err)
			}
			c, err := NewConfig()
			if err != nil {
				t.Fatalf("NewConfig() failed: %v", err)
			}
			if err := c.compare(test.wantConfig); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNewConfigV3SupportNotEnabledOnClient verifies bootstrap functionality
// when the GRPC_XDS_EXPERIMENTAL_V3_SUPPORT environment variable is not enabled
// on the client. In this case, whether the server supports v3 or not, the
// client will end up using v2.
func TestNewConfigV3SupportNotEnabledOnClient(t *testing.T) {
	if err := os.Setenv(v3SupportEnv, "false"); err != nil {
		t.Fatalf("os.Setenv(%s, %s) failed with error: %v", v3SupportEnv, "true", err)
	}
	defer os.Unsetenv(v3SupportEnv)

	cancel := setupBootstrapOverride(v3BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{"serverDoesNotSupportsV3", nonNilCredsConfigV2},
		{"serverSupportsV3", nonNilCredsConfigV2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Setenv(bootstrapFileEnv, test.name); err != nil {
				t.Fatalf("os.Setenv(%s, %s) failed with error: %v", bootstrapFileEnv, test.name, err)
			}
			c, err := NewConfig()
			if err != nil {
				t.Fatalf("NewConfig() failed: %v", err)
			}
			if err := c.compare(test.wantConfig); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNewConfigV3SupportEnabledOnClient verifies bootstrap functionality when
// the GRPC_XDS_EXPERIMENTAL_V3_SUPPORT environment variable is enabled on the
// client. Here the client ends up using v2 or v3 based on what the server
// supports.
func TestNewConfigV3SupportEnabledOnClient(t *testing.T) {
	if err := os.Setenv(v3SupportEnv, "true"); err != nil {
		t.Fatalf("os.Setenv(%s, %s) failed with error: %v", v3SupportEnv, "true", err)
	}
	defer os.Unsetenv(v3SupportEnv)

	cancel := setupBootstrapOverride(v3BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{"serverDoesNotSupportsV3", nonNilCredsConfigV2},
		{"serverSupportsV3", nonNilCredsConfigV3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.Setenv(bootstrapFileEnv, test.name); err != nil {
				t.Fatalf("os.Setenv(%s, %s) failed with error: %v", bootstrapFileEnv, test.name, err)
			}
			c, err := NewConfig()
			if err != nil {
				t.Fatalf("NewConfig() failed: %v", err)
			}
			if err := c.compare(test.wantConfig); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNewConfigBootstrapFileEnvNotSet tests the case where the bootstrap file
// environment variable is not set.
func TestNewConfigBootstrapFileEnvNotSet(t *testing.T) {
	os.Unsetenv(bootstrapFileEnv)
	if _, err := NewConfig(); err == nil {
		t.Errorf("NewConfig() returned nil error, expected to fail")
	}
}
