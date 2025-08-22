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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/jwt"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/xds/bootstrap"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"
)

var (
	v3BootstrapFileMap = map[string]string{
		"serverFeaturesIncludesXDSV3": `
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
				],
				"server_features" : ["xds_v3"]
			}]
		}`,
		"serverFeaturesExcludesXDSV3": `
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
		"emptyNodeProto": `
		{
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "insecure" }
				]
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
					{ "type": "insecure" }
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
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [
					{ "type": "insecure" }
				]
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
					{ "type": "insecure" }
				],
				"unknownField": "foobar"
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
				],
				"server_features": ["xds_v3"]
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
				],
				"server_features": ["xds_v3"]
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
					"channel_creds": [{ "type": "google_default" }],
					"server_features": ["xds_v3"]
				},
				{
					"server_uri": "backup.never.use.com:1234",
					"channel_creds": [{ "type": "google_default" }]
				}
			]
		}`,
		"serverSupportsIgnoreResourceDeletion": `
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
				],
				"server_features" : ["ignore_resource_deletion", "xds_v3"]
			}]
		}`,
		// example data seeded from
		// https://github.com/istio/istio/blob/master/pkg/istio-agent/testdata/grpc-bootstrap.json
		"istioStyleWithJWTCallCreds": `
		{
			"node": {
                "id": "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
                "metadata": {
                  "GENERATOR": "grpc",
                  "INSTANCE_IPS": "127.0.0.1",
                  "ISTIO_VERSION": "1.26.2",
                  "WORKLOAD_IDENTITY_SOCKET_FILE": "socket"
                },
                "locality": {}
			},
			"xds_servers" : [{
				"server_uri": "unix:///etc/istio/XDS",
				"channel_creds": [
					{ "type": "insecure" }
				],
				"call_creds": [
					{ "type": "jwt_token_file", "config": {"jwt_token_file": "/var/run/secrets/tokens/istio-token"} }
				],
				"server_features" : ["xds_v3"]
			}]
		}`,
		"istioStyleWithoutCallCreds": `
		{
			"node": {
                "id": "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
                "metadata": {
                  "GENERATOR": "grpc",
                  "INSTANCE_IPS": "127.0.0.1",
                  "ISTIO_VERSION": "1.26.2",
                  "WORKLOAD_IDENTITY_SOCKET_FILE": "socket"
                },
                "locality": {}
			},
			"xds_servers" : [{
				"server_uri": "unix:///etc/istio/XDS",
				"channel_creds": [
					{ "type": "insecure" }
				],
				"server_features" : ["xds_v3"]
			}]
		}`,
		"istioStyleWithTLSAndJWT": `
		{
			"node": {
                "id": "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
                "metadata": {
                  "GENERATOR": "grpc",
                  "INSTANCE_IPS": "127.0.0.1",
                  "ISTIO_VERSION": "1.26.2",
                  "WORKLOAD_IDENTITY_SOCKET_FILE": "socket"
                },
                "locality": {}
			},
			"xds_servers" : [{
				"server_uri": "unix:///etc/istio/XDS",
				"channel_creds": [
					{ "type": "tls", "config": {} }
				],
				"call_creds": [
					{ "type": "jwt_token_file", "config": {"jwt_token_file": "/var/run/secrets/tokens/istio-token"} }
				],
				"server_features" : ["xds_v3"]
			}]
		}`,
	}
	metadata = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
				Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
			},
		},
	}
	v3Node = node{
		ID:                   "ENVOY_NODE_ID",
		Metadata:             metadata,
		userAgentName:        gRPCUserAgentName,
		userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
		clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
	}
	configWithInsecureCreds = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:     "trafficdirector.googleapis.com:443",
			channelCreds:  []ChannelCreds{{Type: "insecure"}},
			selectedCreds: ChannelCreds{Type: "insecure"},
		}},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}
	configWithMultipleChannelCredsAndV3 = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:      "trafficdirector.googleapis.com:443",
			channelCreds:   []ChannelCreds{{Type: "not-google-default"}, {Type: "google_default"}},
			serverFeatures: []string{"xds_v3"},
			selectedCreds:  ChannelCreds{Type: "google_default"},
		}},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}
	configWithGoogleDefaultCredsAndV3 = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:      "trafficdirector.googleapis.com:443",
			channelCreds:   []ChannelCreds{{Type: "google_default"}},
			serverFeatures: []string{"xds_v3"},
			selectedCreds:  ChannelCreds{Type: "google_default"},
		}},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}
	configWithMultipleServers = &Config{
		xDSServers: []*ServerConfig{
			{
				serverURI:      "trafficdirector.googleapis.com:443",
				channelCreds:   []ChannelCreds{{Type: "google_default"}},
				serverFeatures: []string{"xds_v3"},
				selectedCreds:  ChannelCreds{Type: "google_default"},
			},
			{
				serverURI:     "backup.never.use.com:1234",
				channelCreds:  []ChannelCreds{{Type: "google_default"}},
				selectedCreds: ChannelCreds{Type: "google_default"},
			},
		},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}
	configWithGoogleDefaultCredsAndIgnoreResourceDeletion = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:      "trafficdirector.googleapis.com:443",
			channelCreds:   []ChannelCreds{{Type: "google_default"}},
			serverFeatures: []string{"ignore_resource_deletion", "xds_v3"},
			selectedCreds:  ChannelCreds{Type: "google_default"},
		}},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}
	configWithGoogleDefaultCredsAndNoServerFeatures = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:     "trafficdirector.googleapis.com:443",
			channelCreds:  []ChannelCreds{{Type: "google_default"}},
			selectedCreds: ChannelCreds{Type: "google_default"},
		}},
		node: v3Node,
		clientDefaultListenerResourceNameTemplate: "%s",
	}

	istioNodeMetadata = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"GENERATOR": {
				Kind: &structpb.Value_StringValue{StringValue: "grpc"},
			},
			"INSTANCE_IPS": {
				Kind: &structpb.Value_StringValue{StringValue: "127.0.0.1"},
			},
			"ISTIO_VERSION": {
				Kind: &structpb.Value_StringValue{StringValue: "1.26.2"},
			},
			"WORKLOAD_IDENTITY_SOCKET_FILE": {
				Kind: &structpb.Value_StringValue{StringValue: "socket"},
			},
		},
	}
	jwtCallCreds, _             = jwt.NewTokenFileCallCredentials("/var/run/secrets/tokens/istio-token")
	selectedJWTCallCreds        = []credentials.PerRPCCredentials{jwtCallCreds}
	configWithIstioJWTCallCreds = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:         "unix:///etc/istio/XDS",
			channelCreds:      []ChannelCreds{{Type: "insecure"}},
			callCreds:         []CallCreds{{Type: "jwt_token_file", Config: json.RawMessage("{\n\"jwt_token_file\": \"/var/run/secrets/tokens/istio-token\"\n}")}},
			serverFeatures:    []string{"xds_v3"},
			selectedCreds:     ChannelCreds{Type: "insecure"},
			selectedCallCreds: selectedJWTCallCreds,
		}},
		node: node{
			ID:                   "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
			Metadata:             istioNodeMetadata,
			userAgentName:        gRPCUserAgentName,
			userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
			clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
		},
		certProviderConfigs:                       map[string]*certprovider.BuildableConfig{},
		clientDefaultListenerResourceNameTemplate: "%s",
	}

	configWithIstioStyleNoCallCreds = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:      "unix:///etc/istio/XDS",
			channelCreds:   []ChannelCreds{{Type: "insecure"}},
			serverFeatures: []string{"xds_v3"},
			selectedCreds:  ChannelCreds{Type: "insecure"},
		}},
		node: node{
			ID:                   "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
			Metadata:             istioNodeMetadata,
			userAgentName:        gRPCUserAgentName,
			userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
			clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
		},
		certProviderConfigs:                       map[string]*certprovider.BuildableConfig{},
		clientDefaultListenerResourceNameTemplate: "%s",
	}

	configWithIstioStyleWithTLSAndJWT = &Config{
		xDSServers: []*ServerConfig{{
			serverURI:         "unix:///etc/istio/XDS",
			channelCreds:      []ChannelCreds{{Type: "tls", Config: json.RawMessage("{}")}},
			callCreds:         []CallCreds{{Type: "jwt_token_file", Config: json.RawMessage("{\n\"jwt_token_file\": \"/var/run/secrets/tokens/istio-token\"\n}")}},
			serverFeatures:    []string{"xds_v3"},
			selectedCreds:     ChannelCreds{Type: "tls", Config: json.RawMessage("{}")},
			selectedCallCreds: selectedJWTCallCreds,
		}},
		node: node{
			ID:                   "sidecar~127.0.0.1~pod1.fake-namespace~fake-namespace.svc.cluster.local",
			Metadata:             istioNodeMetadata,
			userAgentName:        gRPCUserAgentName,
			userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
			clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
		},
		certProviderConfigs:                       map[string]*certprovider.BuildableConfig{},
		clientDefaultListenerResourceNameTemplate: "%s",
	}
)

func fileReadFromFileMap(bootstrapFileMap map[string]string, name string) ([]byte, error) {
	if b, ok := bootstrapFileMap[name]; ok {
		return []byte(b), nil
	}
	return nil, os.ErrNotExist
}

func setupBootstrapOverride(bootstrapFileMap map[string]string) func() {
	oldFileReadFunc := bootstrapFileReadFunc
	bootstrapFileReadFunc = func(filename string) ([]byte, error) {
		return fileReadFromFileMap(bootstrapFileMap, filename)
	}
	return func() { bootstrapFileReadFunc = oldFileReadFunc }
}

// This function overrides the bootstrap file NAME env variable, to test the
// code that reads file with the given fileName.
func testGetConfigurationWithFileNameEnv(t *testing.T, fileName string, wantError bool, wantConfig *Config) {
	origBootstrapFileName := envconfig.XDSBootstrapFileName
	envconfig.XDSBootstrapFileName = fileName
	defer func() { envconfig.XDSBootstrapFileName = origBootstrapFileName }()

	c, err := GetConfiguration()
	if (err != nil) != wantError {
		t.Fatalf("GetConfiguration() returned error %v, wantError: %v", err, wantError)
	}
	if wantError {
		return
	}
	if diff := cmp.Diff(wantConfig, c); diff != "" {
		t.Fatalf("Unexpected diff in bootstrap configuration (-want, +got):\n%s", diff)
	}
}

// This function overrides the bootstrap file CONTENT env variable, to test the
// code that uses the content from env directly.
func testGetConfigurationWithFileContentEnv(t *testing.T, fileName string, wantError bool, wantConfig *Config) {
	t.Helper()
	b, err := bootstrapFileReadFunc(fileName)
	if err != nil {
		t.Skip(err)
	}
	origBootstrapContent := envconfig.XDSBootstrapFileContent
	envconfig.XDSBootstrapFileContent = string(b)
	defer func() { envconfig.XDSBootstrapFileContent = origBootstrapContent }()

	c, err := GetConfiguration()
	if (err != nil) != wantError {
		t.Fatalf("GetConfiguration() returned error %v, wantError: %v", err, wantError)
	}
	if wantError {
		return
	}
	if diff := cmp.Diff(wantConfig, c); diff != "" {
		t.Fatalf("Unexpected diff in bootstrap configuration (-want, +got):\n%s", diff)
	}
}

// Tests GetConfiguration with bootstrap file contents that are expected to
// fail.
func (s) TestGetConfiguration_Failure(t *testing.T) {
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
	}
	cancel := setupBootstrapOverride(bootstrapFileMap)
	defer cancel()

	for _, name := range []string{"nonExistentBootstrapFile", "badJSON", "noBalancerName", "emptyXdsServer"} {
		t.Run(name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, name, true, nil)
			testGetConfigurationWithFileContentEnv(t, name, true, nil)
		})
	}
	const name = "empty"
	t.Run(name, func(t *testing.T) {
		testGetConfigurationWithFileNameEnv(t, name, true, nil)
		// If both the env vars are empty, a nil config with a nil error must be
		// returned.
		testGetConfigurationWithFileContentEnv(t, name, false, nil)
	})
}

// Tests the functionality in GetConfiguration with different bootstrap file
// contents. It overrides the fileReadFunc by returning bootstrap file contents
// defined in this test, instead of reading from a file.
func (s) TestGetConfiguration_Success(t *testing.T) {
	cancel := setupBootstrapOverride(v3BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{
			name: "emptyNodeProto",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "insecure"}},
					selectedCreds: ChannelCreds{Type: "insecure"},
				}},
				node: node{
					userAgentName:        gRPCUserAgentName,
					userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
					clientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
				},
				clientDefaultListenerResourceNameTemplate: "%s",
			},
		},
		{"unknownTopLevelFieldInFile", configWithInsecureCreds},
		{"unknownFieldInNodeProto", configWithInsecureCreds},
		{"unknownFieldInXdsServer", configWithInsecureCreds},
		{"multipleChannelCreds", configWithMultipleChannelCredsAndV3},
		{"goodBootstrap", configWithGoogleDefaultCredsAndV3},
		{"multipleXDSServers", configWithMultipleServers},
		{"serverSupportsIgnoreResourceDeletion", configWithGoogleDefaultCredsAndIgnoreResourceDeletion},
		{"istioStyleWithoutCallCreds", configWithIstioStyleNoCallCreds},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, test.name, false, test.wantConfig)
			testGetConfigurationWithFileContentEnv(t, test.name, false, test.wantConfig)
		})
	}
}

// Tests Istio-style bootstrap configurations with JWT call credentials
func (s) TestGetConfiguration_IstioStyleWithCallCreds(t *testing.T) {
	original := envconfig.XDSBootstrapCallCredsEnabled
	envconfig.XDSBootstrapCallCredsEnabled = true
	defer func() {
		envconfig.XDSBootstrapCallCredsEnabled = original
	}()

	cancel := setupBootstrapOverride(v3BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{"istioStyleWithJWTCallCreds", configWithIstioJWTCallCreds},
		{"istioStyleWithTLSAndJWT", configWithIstioStyleWithTLSAndJWT},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, test.name, false, test.wantConfig)
			testGetConfigurationWithFileContentEnv(t, test.name, false, test.wantConfig)
		})
	}
}

// Tests that the two bootstrap env variables are read in correct priority.
//
// "GRPC_XDS_BOOTSTRAP" which specifies the file name containing the bootstrap
// configuration takes precedence over "GRPC_XDS_BOOTSTRAP_CONFIG", which
// directly specifies the bootstrap configuration in itself.
func (s) TestGetConfiguration_BootstrapEnvPriority(t *testing.T) {
	oldFileReadFunc := bootstrapFileReadFunc
	bootstrapFileReadFunc = func(filename string) ([]byte, error) {
		return fileReadFromFileMap(v3BootstrapFileMap, filename)
	}
	defer func() { bootstrapFileReadFunc = oldFileReadFunc }()

	goodFileName1 := "serverFeaturesIncludesXDSV3"
	goodConfig1 := configWithGoogleDefaultCredsAndV3

	goodFileName2 := "serverFeaturesExcludesXDSV3"
	goodFileContent2 := v3BootstrapFileMap[goodFileName2]
	goodConfig2 := configWithGoogleDefaultCredsAndNoServerFeatures

	origBootstrapFileName := envconfig.XDSBootstrapFileName
	envconfig.XDSBootstrapFileName = ""
	defer func() { envconfig.XDSBootstrapFileName = origBootstrapFileName }()

	origBootstrapContent := envconfig.XDSBootstrapFileContent
	envconfig.XDSBootstrapFileContent = ""
	defer func() { envconfig.XDSBootstrapFileContent = origBootstrapContent }()

	// When both env variables are empty, GetConfiguration should return nil.
	if cfg, err := GetConfiguration(); err != nil || cfg != nil {
		t.Errorf("GetConfiguration() returned (%v, %v), want (<nil>, <nil>)", cfg, err)
	}

	// When one of them is set, it should be used.
	envconfig.XDSBootstrapFileName = goodFileName1
	envconfig.XDSBootstrapFileContent = ""
	c, err := GetConfiguration()
	if err != nil {
		t.Errorf("GetConfiguration() failed: %v", err)
	}
	if diff := cmp.Diff(goodConfig1, c); diff != "" {
		t.Errorf("Unexpected diff in bootstrap configuration (-want, +got):\n%s", diff)
	}

	envconfig.XDSBootstrapFileName = ""
	envconfig.XDSBootstrapFileContent = goodFileContent2
	c, err = GetConfiguration()
	if err != nil {
		t.Errorf("GetConfiguration() failed: %v", err)
	}
	if diff := cmp.Diff(goodConfig2, c); diff != "" {
		t.Errorf("Unexpected diff in bootstrap configuration (-want, +got):\n%s", diff)
	}

	// Set both, file name should be read.
	envconfig.XDSBootstrapFileName = goodFileName1
	envconfig.XDSBootstrapFileContent = goodFileContent2
	c, err = GetConfiguration()
	if err != nil {
		t.Errorf("GetConfiguration() failed: %v", err)
	}
	if diff := cmp.Diff(goodConfig1, c); diff != "" {
		t.Errorf("Unexpected diff in bootstrap configuration (-want, +got):\n%s", diff)
	}
}

func init() {
	certprovider.Register(&fakeCertProviderBuilder{})
}

const fakeCertProviderName = "fake-certificate-provider"

// fakeCertProviderBuilder builds new instances of fakeCertProvider and
// interprets the config provided to it as JSON with a single key and value.
type fakeCertProviderBuilder struct{}

// ParseConfig expects input in JSON format containing a map from string to
// string, with a single entry and mapKey being "configKey".
func (b *fakeCertProviderBuilder) ParseConfig(cfg any) (*certprovider.BuildableConfig, error) {
	config, ok := cfg.(json.RawMessage)
	if !ok {
		return nil, fmt.Errorf("fakeCertProviderBuilder received config of type %T, want []byte", config)
	}
	var cfgData map[string]string
	if err := json.Unmarshal(config, &cfgData); err != nil {
		return nil, fmt.Errorf("fakeCertProviderBuilder config parsing failed: %v", err)
	}
	if len(cfgData) != 1 || cfgData["configKey"] == "" {
		return nil, errors.New("fakeCertProviderBuilder received invalid config")
	}
	fc := &fakeStableConfig{config: cfgData}
	return certprovider.NewBuildableConfig(fakeCertProviderName, fc.canonical(), func(certprovider.BuildOptions) certprovider.Provider {
		return &fakeCertProvider{}
	}), nil
}

func (b *fakeCertProviderBuilder) Name() string {
	return fakeCertProviderName
}

type fakeStableConfig struct {
	config map[string]string
}

func (c *fakeStableConfig) canonical() []byte {
	var cfg string
	for k, v := range c.config {
		cfg = fmt.Sprintf("%s:%s", k, v)
	}
	return []byte(cfg)
}

// fakeCertProvider is an empty implementation of the Provider interface.
type fakeCertProvider struct {
	certprovider.Provider
}

func (s) TestGetConfiguration_CertificateProviders(t *testing.T) {
	bootstrapFileMap := map[string]string{
		"badJSONCertProviderConfig": `
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
				],
				"server_features" : ["foo", "bar", "xds_v3"],
			}],
			"certificate_providers": "bad JSON"
		}`,
		"allUnknownCertProviders": `
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
				],
				"server_features" : ["xds_v3"]
			}],
			"certificate_providers": {
				"unknownProviderInstance1": {
					"plugin_name": "foo",
					"config": {"foo": "bar"}
				},
				"unknownProviderInstance2": {
					"plugin_name": "bar",
					"config": {"foo": "bar"}
				}
			}
		}`,
		"badCertProviderConfig": `
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
				],
				"server_features" : ["xds_v3"],
			}],
			"certificate_providers": {
				"unknownProviderInstance": {
					"plugin_name": "foo",
					"config": {"foo": "bar"}
				},
				"fakeProviderInstanceBad": {
					"plugin_name": "fake-certificate-provider",
					"config": {"configKey": 666}
				}
			}
		}`,
		"goodCertProviderConfig": `
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
					{ "type": "insecure" }
				],
				"server_features" : ["xds_v3"]
			}],
			"certificate_providers": {
				"unknownProviderInstance": {
					"plugin_name": "foo",
					"config": {"foo": "bar"}
				},
				"fakeProviderInstance": {
					"plugin_name": "fake-certificate-provider",
					"config": {"configKey": "configValue"}
				}
			}
		}`,
	}

	getBuilder := internal.GetCertificateProviderBuilder.(func(string) certprovider.Builder)
	parser := getBuilder(fakeCertProviderName)
	if parser == nil {
		t.Fatalf("Missing certprovider plugin %q", fakeCertProviderName)
	}
	wantCfg, err := parser.ParseConfig(json.RawMessage(`{"configKey": "configValue"}`))
	if err != nil {
		t.Fatalf("config parsing for plugin %q failed: %v", fakeCertProviderName, err)
	}

	cancel := setupBootstrapOverride(bootstrapFileMap)
	defer cancel()

	goodConfig := &Config{
		xDSServers: []*ServerConfig{{
			serverURI:      "trafficdirector.googleapis.com:443",
			channelCreds:   []ChannelCreds{{Type: "insecure"}},
			serverFeatures: []string{"xds_v3"},
			selectedCreds:  ChannelCreds{Type: "insecure"},
		}},
		certProviderConfigs: map[string]*certprovider.BuildableConfig{
			"fakeProviderInstance": wantCfg,
		},
		clientDefaultListenerResourceNameTemplate: "%s",
		node: v3Node,
	}
	tests := []struct {
		name       string
		wantConfig *Config
		wantErr    bool
	}{
		{
			name:    "badJSONCertProviderConfig",
			wantErr: true,
		},
		{

			name:    "badCertProviderConfig",
			wantErr: true,
		},
		{

			name:       "allUnknownCertProviders",
			wantConfig: configWithGoogleDefaultCredsAndV3,
		},
		{
			name:       "goodCertProviderConfig",
			wantConfig: goodConfig,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testGetConfigurationWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func (s) TestGetConfiguration_ServerListenerResourceNameTemplate(t *testing.T) {
	cancel := setupBootstrapOverride(map[string]string{
		"badServerListenerResourceNameTemplate:": `
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
			"server_listener_resource_name_template": 123456789
		}`,
		"goodServerListenerResourceNameTemplate": `
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
			"server_listener_resource_name_template": "grpc/server?xds.resource.listening_address=%s"
		}`,
	})
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
		wantErr    bool
	}{
		{
			name:    "badServerListenerResourceNameTemplate",
			wantErr: true,
		},
		{
			name: "goodServerListenerResourceNameTemplate",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "google_default"}},
					selectedCreds: ChannelCreds{Type: "google_default"},
				}},
				node:                               v3Node,
				serverListenerResourceNameTemplate: "grpc/server?xds.resource.listening_address=%s",
				clientDefaultListenerResourceNameTemplate: "%s",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testGetConfigurationWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func (s) TestGetConfiguration_Federation(t *testing.T) {
	cancel := setupBootstrapOverride(map[string]string{
		"badclientListenerResourceNameTemplate": `
		{
			"node": { "id": "ENVOY_NODE_ID" },
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443"
			}],
			"client_default_listener_resource_name_template": 123456789
		}`,
		"badclientListenerResourceNameTemplatePerAuthority": `
		{
			"node": { "id": "ENVOY_NODE_ID" },
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [ { "type": "google_default" } ]
			}],
			"authorities": {
				"xds.td.com": {
					"client_listener_resource_name_template": "some/template/%s",
					"xds_servers": [{
						"server_uri": "td.com",
						"channel_creds": [ { "type": "google_default" } ],
						"server_features" : ["foo", "bar", "xds_v3"]
					}]
				}
			}
		}`,
		"good": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [ { "type": "google_default" } ]
			}],
			"server_listener_resource_name_template": "xdstp://xds.example.com/envoy.config.listener.v3.Listener/grpc/server?listening_address=%s",
			"client_default_listener_resource_name_template": "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
			"authorities": {
				"xds.td.com": {
					"client_listener_resource_name_template": "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
					"xds_servers": [{
						"server_uri": "td.com",
						"channel_creds": [ { "type": "google_default" } ],
						"server_features" : ["xds_v3"]
					}]
				}
			}
		}`,
		// If client_default_listener_resource_name_template is not set, it
		// defaults to "%s".
		"goodWithDefaultDefaultClientListenerTemplate": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [ { "type": "google_default" } ]
			}]
		}`,
		// If client_listener_resource_name_template in authority is not set, it
		// defaults to
		// "xdstp://<authority_name>/envoy.config.listener.v3.Listener/%s".
		"goodWithDefaultClientListenerTemplatePerAuthority": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [ { "type": "google_default" } ]
			}],
			"client_default_listener_resource_name_template": "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
			"authorities": {
				"xds.td.com": { },
				"#.com": { }
			}
		}`,
		// It's OK for an authority to not have servers. The top-level server
		// will be used.
		"goodWithNoServerPerAuthority": `
		{
			"node": {
				"id": "ENVOY_NODE_ID",
				"metadata": {
				    "TRAFFICDIRECTOR_GRPC_HOSTNAME": "trafficdirector"
			    }
			},
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443",
				"channel_creds": [ { "type": "google_default" } ]
			}],
			"client_default_listener_resource_name_template": "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
			"authorities": {
				"xds.td.com": {
					"client_listener_resource_name_template": "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s"
				}
			}
		}`,
	})
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
		wantErr    bool
	}{
		{
			name:    "badclientListenerResourceNameTemplate",
			wantErr: true,
		},
		{
			name:    "badclientListenerResourceNameTemplatePerAuthority",
			wantErr: true,
		},
		{
			name: "good",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "google_default"}},
					selectedCreds: ChannelCreds{Type: "google_default"},
				}},
				node:                               v3Node,
				serverListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/grpc/server?listening_address=%s",
				clientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				authorities: map[string]*Authority{
					"xds.td.com": {
						ClientListenerResourceNameTemplate: "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
						XDSServers: []*ServerConfig{{
							serverURI:      "td.com",
							channelCreds:   []ChannelCreds{{Type: "google_default"}},
							serverFeatures: []string{"xds_v3"},
							selectedCreds:  ChannelCreds{Type: "google_default"},
						}},
					},
				},
			},
		},
		{
			name: "goodWithDefaultDefaultClientListenerTemplate",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "google_default"}},
					selectedCreds: ChannelCreds{Type: "google_default"},
				}},
				node: v3Node,
				clientDefaultListenerResourceNameTemplate: "%s",
			},
		},
		{
			name: "goodWithDefaultClientListenerTemplatePerAuthority",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "google_default"}},
					selectedCreds: ChannelCreds{Type: "google_default"},
				}},
				node: v3Node,
				clientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				authorities: map[string]*Authority{
					"xds.td.com": {
						ClientListenerResourceNameTemplate: "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
					},
					"#.com": {
						ClientListenerResourceNameTemplate: "xdstp://%23.com/envoy.config.listener.v3.Listener/%s",
					},
				},
			},
		},
		{
			name: "goodWithNoServerPerAuthority",
			wantConfig: &Config{
				xDSServers: []*ServerConfig{{
					serverURI:     "trafficdirector.googleapis.com:443",
					channelCreds:  []ChannelCreds{{Type: "google_default"}},
					selectedCreds: ChannelCreds{Type: "google_default"},
				}},
				node: v3Node,
				clientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				authorities: map[string]*Authority{
					"xds.td.com": {
						ClientListenerResourceNameTemplate: "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testGetConfigurationWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testGetConfigurationWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func (s) TestServerConfigMarshalAndUnmarshal(t *testing.T) {
	origConfig, err := ServerConfigForTesting(ServerConfigTestingOptions{URI: "test-server", ServerFeatures: []string{"xds_v3"}})
	if err != nil {
		t.Fatalf("Failed to create server config for testing: %v", err)
	}
	marshaledCfg, err := json.Marshal(origConfig)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	unmarshaledConfig := new(ServerConfig)
	if err := json.Unmarshal(marshaledCfg, unmarshaledConfig); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if diff := cmp.Diff(origConfig, unmarshaledConfig); diff != "" {
		t.Fatalf("Unexpected diff in server config (-want, +got):\n%s", diff)
	}
}

func (s) TestDefaultBundles(t *testing.T) {
	tests := []string{"google_default", "insecure", "tls"}

	for _, typename := range tests {
		t.Run(typename, func(t *testing.T) {
			if c := bootstrap.GetCredentials(typename); c == nil {
				t.Errorf(`bootstrap.GetCredentials(%s) credential is nil, want non-nil`, typename)
			}
		})
	}
}

func (s) TestCallCreds_Equal(t *testing.T) {
	tests := []struct {
		name   string
		cc1    CallCreds
		cc2    CallCreds
		expect bool
	}{
		{
			name:   "identical configs",
			cc1:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			cc2:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			expect: true,
		},
		{
			name:   "different types",
			cc1:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			cc2:    CallCreds{Type: "other_type", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			expect: false,
		},
		{
			name:   "different configs",
			cc1:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			cc2:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/different/path"}`)},
			expect: false,
		},
		{
			name:   "nil vs non-nil configs",
			cc1:    CallCreds{Type: "jwt_token_file", Config: nil},
			cc2:    CallCreds{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/path/to/token"}`)},
			expect: false,
		},
		{
			name:   "both nil configs",
			cc1:    CallCreds{Type: "jwt_token_file", Config: nil},
			cc2:    CallCreds{Type: "jwt_token_file", Config: nil},
			expect: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := test.cc1.Equal(test.cc2)
			if result != test.expect {
				t.Errorf("CallCreds.Equal() = %v, want %v", result, test.expect)
			}
		})
	}
}

func (s) TestServerConfig_UnmarshalJSON_WithCallCreds(t *testing.T) {
	original := envconfig.XDSBootstrapCallCredsEnabled
	defer func() { envconfig.XDSBootstrapCallCredsEnabled = original }()
	envconfig.XDSBootstrapCallCredsEnabled = true
	tests := []struct {
		name          string
		json          string
		wantCallCreds []CallCreds
		errContains   string
	}{
		{
			name: "valid call_creds with jwt_token_file",
			json: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}],
				"call_creds": [
					{
						"type": "jwt_token_file",
						"config": {"jwt_token_file": "/path/to/token.jwt"}
					}
				]
			}`,
			wantCallCreds: []CallCreds{{
				Type:   "jwt_token_file",
				Config: json.RawMessage(`{"jwt_token_file": "/path/to/token.jwt"}`),
			}},
		},
		{
			name: "multiple call_creds types",
			json: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}],
				"call_creds": [
					{"type": "jwt_token_file", "config": {"jwt_token_file": "/token1.jwt"}},
					{"type": "unsupported_type", "config": {}}
				]
			}`,
			wantCallCreds: []CallCreds{
				{Type: "jwt_token_file", Config: json.RawMessage(`{"jwt_token_file": "/token1.jwt"}`)},
				{Type: "unsupported_type", Config: json.RawMessage(`{}`)},
			},
		},
		{
			name: "empty call_creds array",
			json: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}],
				"call_creds": []
			}`,
			wantCallCreds: []CallCreds{},
		},
		{
			name: "missing call_creds field",
			json: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}]
			}`,
			wantCallCreds: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var sc ServerConfig
			err := sc.UnmarshalJSON([]byte(test.json))

			if test.errContains != "" {
				if err == nil {
					t.Fatal("Expected error, got nil")
				}
				if test.errContains != "" && !strings.Contains(err.Error(), test.errContains) {
					t.Errorf("Error %v should contain %q", err, test.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if diff := cmp.Diff(test.wantCallCreds, sc.CallCreds()); diff != "" {
				t.Errorf("CallCreds mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func (s) TestServerConfig_Equal_WithCallCreds(t *testing.T) {
	callCreds := []CallCreds{{
		Type:   "jwt_token_file",
		Config: json.RawMessage(`{"jwt_token_file": "/test/token.jwt"}`),
	}}
	sc1 := &ServerConfig{
		serverURI:      "server1",
		channelCreds:   []ChannelCreds{{Type: "insecure"}},
		callCreds:      callCreds,
		serverFeatures: []string{"feature1"},
	}
	sc2 := &ServerConfig{
		serverURI:      "server1",
		channelCreds:   []ChannelCreds{{Type: "insecure"}},
		callCreds:      callCreds,
		serverFeatures: []string{"feature1"},
	}
	sc3 := &ServerConfig{
		serverURI:      "server1",
		channelCreds:   []ChannelCreds{{Type: "insecure"}},
		callCreds:      []CallCreds{{Type: "different"}},
		serverFeatures: []string{"feature1"},
	}

	if !sc1.Equal(sc2) {
		t.Error("Equal ServerConfigs with same call creds should be equal")
	}
	if sc1.Equal(sc3) {
		t.Error("ServerConfigs with different call creds should not be equal")
	}
}

func (s) TestServerConfig_MarshalJSON_WithCallCreds(t *testing.T) {
	original := envconfig.XDSBootstrapCallCredsEnabled
	defer func() { envconfig.XDSBootstrapCallCredsEnabled = original }()
	envconfig.XDSBootstrapCallCredsEnabled = true
	sc := &ServerConfig{
		serverURI:    "test-server:443",
		channelCreds: []ChannelCreds{{Type: "insecure"}},
		callCreds: []CallCreds{{
			Type:   "jwt_token_file",
			Config: json.RawMessage(`{"jwt_token_file":"/test/token.jwt"}`),
		}},
		serverFeatures: []string{"test_feature"},
	}

	data, err := sc.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	// Check Marshal/Unmarshal symmetry.
	var unmarshaled ServerConfig
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if diff := cmp.Diff(sc.CallCreds(), unmarshaled.CallCreds()); diff != "" {
		t.Errorf("Marshal/Unmarshal call credentials produces differences:\n%s", diff)
	}
}

func newStructProtoFromMap(t *testing.T, input map[string]any) *structpb.Struct {
	t.Helper()

	ret, err := structpb.NewStruct(input)
	if err != nil {
		t.Fatalf("Failed to create new struct proto from map %v: %v", input, err)
	}
	return ret
}

func (s) TestNode_MarshalAndUnmarshal(t *testing.T) {
	tests := []struct {
		desc      string
		inputJSON []byte
		wantNode  node
	}{
		{
			desc: "basic happy case",
			inputJSON: []byte(`{
  "id": "id",
  "cluster": "cluster",
  "locality": {
    "region": "region",
    "zone": "zone",
    "sub_zone": "sub_zone"
  },
  "metadata": {
	"k1": "v1",
	"k2": 101,
	"k3": 280.0
  }
}`),
			wantNode: node{
				ID:      "id",
				Cluster: "cluster",
				Locality: locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				},
				Metadata: newStructProtoFromMap(t, map[string]any{
					"k1": "v1",
					"k2": 101,
					"k3": 280.0,
				}),
				userAgentName:        "gRPC Go",
				userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
				clientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
			},
		},
		{
			desc: "client controlled fields",
			inputJSON: []byte(`{
  "id": "id",
  "cluster": "cluster",
  "user_agent_name": "user_agent_name",
  "user_agent_version_type": {
	"user_agent_version": "version"
  },
  "client_features": ["feature1", "feature2"]
}`),
			wantNode: node{
				ID:                   "id",
				Cluster:              "cluster",
				userAgentName:        "gRPC Go",
				userAgentVersionType: userAgentVersion{UserAgentVersion: grpc.Version},
				clientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			// Unmarshal the input JSON into a node struct and check if it
			// matches expectations.
			unmarshaledNode := newNode()
			if err := json.Unmarshal([]byte(test.inputJSON), &unmarshaledNode); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.wantNode, unmarshaledNode); diff != "" {
				t.Fatalf("Unexpected diff in node: (-want, +got):\n%s", diff)
			}

			// Marshal the recently unmarshaled node struct into JSON and
			// remarshal it into another node struct, and check that it still
			// matches expectations.
			marshaledJSON, err := json.Marshal(unmarshaledNode)
			if err != nil {
				t.Fatalf("node.MarshalJSON() failed: %v", err)
			}
			reUnmarshaledNode := newNode()
			if err := json.Unmarshal([]byte(marshaledJSON), &reUnmarshaledNode); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(test.wantNode, reUnmarshaledNode); diff != "" {
				t.Fatalf("Unexpected diff in node: (-want, +got):\n%s", diff)
			}
		})
	}
}

func (s) TestNode_ToProto(t *testing.T) {
	tests := []struct {
		desc      string
		inputNode node
		wantProto *v3corepb.Node
	}{
		{
			desc: "all fields set",
			inputNode: func() node {
				n := newNode()
				n.ID = "id"
				n.Cluster = "cluster"
				n.Locality = locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				}
				n.Metadata = newStructProtoFromMap(t, map[string]any{
					"k1": "v1",
					"k2": 101,
					"k3": 280.0,
				})
				return n
			}(),
			wantProto: &v3corepb.Node{
				Id:      "id",
				Cluster: "cluster",
				Locality: &v3corepb.Locality{
					Region:  "region",
					Zone:    "zone",
					SubZone: "sub_zone",
				},
				Metadata: newStructProtoFromMap(t, map[string]any{
					"k1": "v1",
					"k2": 101,
					"k3": 280.0,
				}),
				UserAgentName:        "gRPC Go",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
				ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
			},
		},
		{
			desc: "some fields unset",
			inputNode: func() node {
				n := newNode()
				n.ID = "id"
				return n
			}(),
			wantProto: &v3corepb.Node{
				Id:                   "id",
				UserAgentName:        "gRPC Go",
				UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
				ClientFeatures:       []string{"envoy.lb.does_not_support_overprovisioning", "xds.config.resource-in-sotw"},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			gotProto := test.inputNode.toProto()
			if diff := cmp.Diff(test.wantProto, gotProto, protocmp.Transform()); diff != "" {
				t.Fatalf("Unexpected diff in node proto: (-want, +got):\n%s", diff)
			}
		})
	}
}

func (s) TestBootstrap_SelectedCredsAndCallCreds(t *testing.T) {
	original := envconfig.XDSBootstrapCallCredsEnabled
	envconfig.XDSBootstrapCallCredsEnabled = true
	defer func() {
		envconfig.XDSBootstrapCallCredsEnabled = original
	}()

	tokenFile := "/token.jwt"
	tests := []struct {
		name                string
		bootstrapConfig     string
		expectCallCreds     int
		expectTransportType string
	}{
		{
			name: "JWT call creds with TLS channel creds",
			bootstrapConfig: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "tls", "config": {}}],
				"call_creds": [
					{
						"type": "jwt_token_file",
						"config": {"jwt_token_file": "` + tokenFile + `"}
					}
				]
			}`,
			expectCallCreds:     1,
			expectTransportType: "tls",
		},
		{
			name: "JWT call creds with multiple channel creds",
			bootstrapConfig: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "tls", "config": {}}, {"type": "insecure"}],
				"call_creds": [
					{
						"type": "jwt_token_file",
						"config": {"jwt_token_file": "` + tokenFile + `"}
					},
					{
						"type": "jwt_token_file",
						"config": {"jwt_token_file": "` + tokenFile + `"}
					}
				]
			}`,
			expectCallCreds:     2,
			expectTransportType: "tls", // the first channel creds is selected
		},
		{
			name: "JWT call creds with insecure channel creds",
			bootstrapConfig: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}],
				"call_creds": [
					{
						"type": "jwt_token_file",
						"config": {"jwt_token_file": "` + tokenFile + `"}
					}
				]
			}`,
			expectCallCreds:     1,
			expectTransportType: "insecure",
		},
		{
			name: "No call creds",
			bootstrapConfig: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}]
			}`,
			expectCallCreds:     0,
			expectTransportType: "insecure",
		},
		{
			name: "No call creds multiple channel creds",
			bootstrapConfig: `{
				"server_uri": "xds-server:443",
				"channel_creds": [{"type": "insecure"}, {"type": "tls", "config": {}}]
			}`,
			expectCallCreds:     0,
			expectTransportType: "insecure",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var sc ServerConfig
			err := sc.UnmarshalJSON([]byte(test.bootstrapConfig))
			if err != nil {
				t.Fatalf("Failed to unmarshal bootstrap config: %v", err)
			}

			// Verify call credentials processing.
			callCreds := sc.CallCreds()
			selectedCallCreds := sc.SelectedCallCreds()

			if len(callCreds) != test.expectCallCreds {
				t.Errorf("Call creds count = %d, want %d", len(callCreds), test.expectCallCreds)
			}
			if len(selectedCallCreds) != test.expectCallCreds {
				t.Errorf("Selected call creds count = %d, want %d", len(selectedCallCreds), test.expectCallCreds)
			}

			// Verify transport credentials are properly selected.
			if sc.SelectedCreds().Type != test.expectTransportType {
				t.Errorf("Selected transport creds type = %q, want %q",
					sc.SelectedCreds().Type, test.expectTransportType)
			}
		})
	}
}

func (s) TestDialOptionsWithCallCredsForTransport(t *testing.T) {
	testJWTCreds := &testPerRPCCreds{requireSecurity: true}
	testInsecureCreds := &testPerRPCCreds{requireSecurity: false}

	sc := &ServerConfig{
		selectedCallCreds: []credentials.PerRPCCredentials{
			testJWTCreds,
			testInsecureCreds,
		},
		extraDialOptions: []grpc.DialOption{
			grpc.WithUserAgent("test-agent"), // extra option
		},
	}

	tests := []struct {
		name             string
		transportType    string
		transportCreds   credentials.TransportCredentials
		expectJWTCreds   bool
		expectOtherCreds bool
	}{
		{
			name:             "insecure transport by type",
			transportType:    "insecure",
			transportCreds:   nil,
			expectJWTCreds:   false,
			expectOtherCreds: true,
		},
		{
			name:             "insecure transport by protocol",
			transportType:    "custom",
			transportCreds:   insecure.NewCredentials(),
			expectJWTCreds:   false,
			expectOtherCreds: true,
		},
		{
			name:             "secure transport",
			transportType:    "tls",
			transportCreds:   &testTransportCreds{securityProtocol: "tls"},
			expectJWTCreds:   true,
			expectOtherCreds: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := sc.DialOptionsWithCallCredsForTransport(test.transportType, test.transportCreds)

			// Count dial options (should include extra options + applicable
			// call creds)
			expectedCount := 2
			if test.expectJWTCreds {
				expectedCount++
			}

			if len(opts) != expectedCount {
				t.Errorf("DialOptions count = %d, want %d", len(opts), expectedCount)
			}
		})
	}
}

type testPerRPCCreds struct {
	requireSecurity bool
}

func (c *testPerRPCCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"test": "metadata"}, nil
}

func (c *testPerRPCCreds) RequireTransportSecurity() bool {
	return c.requireSecurity
}

type testTransportCreds struct {
	securityProtocol string
}

func (c *testTransportCreds) ClientHandshake(_ context.Context, _ string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, &testAuthInfo{}, nil
}

func (c *testTransportCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, &testAuthInfo{}, nil
}

func (c *testTransportCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: c.securityProtocol}
}

func (c *testTransportCreds) Clone() credentials.TransportCredentials {
	return &testTransportCreds{securityProtocol: c.securityProtocol}
}

func (c *testTransportCreds) OverrideServerName(string) error {
	return nil
}

type testAuthInfo struct{}

func (a *testAuthInfo) AuthType() string {
	return "test"
}

func (a *testAuthInfo) GetCommonAuthInfo() credentials.CommonAuthInfo {
	return credentials.CommonAuthInfo{}
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}
