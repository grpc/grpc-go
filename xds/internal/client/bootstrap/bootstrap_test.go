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
	"encoding/json"
	"errors"
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/xds/internal/env"
	"google.golang.org/grpc/xds/internal/version"
)

var (
	v2BootstrapFileMap = map[string]string{
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
				],
				"server_features" : ["foo", "bar"]
			}]
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
				],
				"server_features" : ["foo", "bar", "xds_v3"]
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
		Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
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
	if c.ServerResourceNameID != want.ServerResourceNameID {
		return fmt.Errorf("config.ServerResourceNameID is %q, want %q", c.ServerResourceNameID, want.ServerResourceNameID)
	}

	// A vanilla cmp.Equal or cmp.Diff will not produce useful error message
	// here. So, we iterate through the list of configs and compare them one at
	// a time.
	gotCfgs := c.CertProviderConfigs
	wantCfgs := want.CertProviderConfigs
	if len(gotCfgs) != len(wantCfgs) {
		return fmt.Errorf("config.CertProviderConfigs is %d entries, want %d", len(gotCfgs), len(wantCfgs))
	}
	for instance, gotCfg := range gotCfgs {
		wantCfg, ok := wantCfgs[instance]
		if !ok {
			return fmt.Errorf("config.CertProviderConfigs has unexpected plugin instance %q with config %q", instance, gotCfg.String())
		}
		if got, want := gotCfg.String(), wantCfg.String(); got != want {
			return fmt.Errorf("config.CertProviderConfigs for plugin instance %q has config %q, want %q", instance, got, want)
		}
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
	return func() { bootstrapFileReadFunc = oldFileReadFunc }
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
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

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
				Creds:        grpc.WithTransportCredentials(insecure.NewCredentials()),
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
		{"multipleChannelCreds", nonNilCredsConfigV2},
		{"goodBootstrap", nonNilCredsConfigV2},
		{"multipleXDSServers", nonNilCredsConfigV2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

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
	origV3Support := env.V3Support
	env.V3Support = false
	defer func() { env.V3Support = origV3Support }()

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
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

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
	origV3Support := env.V3Support
	env.V3Support = true
	defer func() { env.V3Support = origV3Support }()

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
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

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
	origBootstrapFileName := env.BootstrapFileName
	env.BootstrapFileName = ""
	defer func() { env.BootstrapFileName = origBootstrapFileName }()

	if _, err := NewConfig(); err == nil {
		t.Errorf("NewConfig() returned nil error, expected to fail")
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
func (b *fakeCertProviderBuilder) ParseConfig(cfg interface{}) (*certprovider.BuildableConfig, error) {
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

func TestNewConfigWithCertificateProviders(t *testing.T) {
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
				"server_features" : ["foo", "bar", "xds_v3"]
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
				"server_features" : ["foo", "bar", "xds_v3"],
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
					{ "type": "google_default" }
				],
				"server_features" : ["foo", "bar", "xds_v3"]
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
		t.Fatalf("missing certprovider plugin %q", fakeCertProviderName)
	}
	wantCfg, err := parser.ParseConfig(json.RawMessage(`{"configKey": "configValue"}`))
	if err != nil {
		t.Fatalf("config parsing for plugin %q failed: %v", fakeCertProviderName, err)
	}

	origV3Support := env.V3Support
	env.V3Support = true
	defer func() { env.V3Support = origV3Support }()

	cancel := setupBootstrapOverride(bootstrapFileMap)
	defer cancel()

	goodConfig := &Config{
		BalancerName: "trafficdirector.googleapis.com:443",
		Creds:        grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
		TransportAPI: version.TransportV3,
		NodeProto:    v3NodeProto,
		CertProviderConfigs: map[string]*certprovider.BuildableConfig{
			"fakeProviderInstance": wantCfg,
		},
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
			wantConfig: nonNilCredsConfigV3,
		},
		{
			name:       "goodCertProviderConfig",
			wantConfig: goodConfig,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

			c, err := NewConfig()
			if (err != nil) != test.wantErr {
				t.Fatalf("NewConfig() returned: %v, wantErr: %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if err := c.compare(test.wantConfig); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewConfigWithServerResourceNameID(t *testing.T) {
	cancel := setupBootstrapOverride(map[string]string{
		"badServerResourceNameID": `
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
			"grpc_server_resource_name_id": 123456789
		}`,
		"goodServerResourceNameID": `
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
			"grpc_server_resource_name_id": "grpc/server"
		}`,
	})
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
		wantErr    bool
	}{
		{
			name:    "badServerResourceNameID",
			wantErr: true,
		},
		{
			name: "goodServerResourceNameID",
			wantConfig: &Config{
				BalancerName:         "trafficdirector.googleapis.com:443",
				Creds:                grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()),
				TransportAPI:         version.TransportV2,
				NodeProto:            v2NodeProto,
				ServerResourceNameID: "grpc/server",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origBootstrapFileName := env.BootstrapFileName
			env.BootstrapFileName = test.name
			defer func() { env.BootstrapFileName = origBootstrapFileName }()

			c, err := NewConfig()
			if (err != nil) != test.wantErr {
				t.Fatalf("NewConfig() returned (%+v, %v), wantErr: %v", c, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if err := c.compare(test.wantConfig); err != nil {
				t.Fatal(err)
			}
		})
	}
}
