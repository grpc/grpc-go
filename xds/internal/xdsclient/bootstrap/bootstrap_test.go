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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/xds/bootstrap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
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
					"channel_creds": [{ "type": "not-google-default" }]
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
	}
	metadata = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"TRAFFICDIRECTOR_GRPC_HOSTNAME": {
				Kind: &structpb.Value_StringValue{StringValue: "trafficdirector"},
			},
		},
	}
	v3NodeProto = &v3corepb.Node{
		Id:                   "ENVOY_NODE_ID",
		Metadata:             metadata,
		UserAgentName:        gRPCUserAgentName,
		UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
		ClientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
	}
	nilCredsConfigNoServerFeatures = &Config{
		XDSServer: &ServerConfig{
			ServerURI: "trafficdirector.googleapis.com:443",
			Creds:     ChannelCreds{Type: "insecure"},
		},
		NodeProto: v3NodeProto,
		ClientDefaultListenerResourceNameTemplate: "%s",
	}
	nonNilCredsConfigV3 = &Config{
		XDSServer: &ServerConfig{
			ServerURI:      "trafficdirector.googleapis.com:443",
			Creds:          ChannelCreds{Type: "google_default"},
			ServerFeatures: []string{"xds_v3"},
		},
		NodeProto: v3NodeProto,
		ClientDefaultListenerResourceNameTemplate: "%s",
	}
	nonNilCredsConfigWithDeletionIgnored = &Config{
		XDSServer: &ServerConfig{
			ServerURI:              "trafficdirector.googleapis.com:443",
			Creds:                  ChannelCreds{Type: "google_default"},
			IgnoreResourceDeletion: true,
			ServerFeatures:         []string{"ignore_resource_deletion", "xds_v3"},
		},
		NodeProto: v3NodeProto,
		ClientDefaultListenerResourceNameTemplate: "%s",
	}
	nonNilCredsConfigNoServerFeatures = &Config{
		XDSServer: &ServerConfig{
			ServerURI: "trafficdirector.googleapis.com:443",
			Creds:     ChannelCreds{Type: "google_default"},
		},
		NodeProto: v3NodeProto,
		ClientDefaultListenerResourceNameTemplate: "%s",
	}
)

func (c *Config) compare(want *Config) error {
	if diff := cmp.Diff(want, c,
		cmpopts.EquateEmpty(),
		cmp.Comparer(proto.Equal),
		cmp.Comparer(func(a, b grpc.DialOption) bool { return (a != nil) == (b != nil) }),
		cmp.Transformer("certproviderconfigstring", func(a *certprovider.BuildableConfig) string { return a.String() }),
	); diff != "" {
		return fmt.Errorf("unexpected diff in config (-want, +got):\n%s", diff)
	}
	return nil
}

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

// TODO: enable leak check for this package when
// https://github.com/googleapis/google-cloud-go/issues/2417 is fixed.

// This function overrides the bootstrap file NAME env variable, to test the
// code that reads file with the given fileName.
func testNewConfigWithFileNameEnv(t *testing.T, fileName string, wantError bool, wantConfig *Config) {
	origBootstrapFileName := envconfig.XDSBootstrapFileName
	envconfig.XDSBootstrapFileName = fileName
	defer func() { envconfig.XDSBootstrapFileName = origBootstrapFileName }()

	c, err := NewConfig()
	if (err != nil) != wantError {
		t.Fatalf("NewConfig() returned error %v, wantError: %v", err, wantError)
	}
	if wantError {
		return
	}
	if err := c.compare(wantConfig); err != nil {
		t.Fatal(err)
	}
}

// This function overrides the bootstrap file CONTENT env variable, to test the
// code that uses the content from env directly.
func testNewConfigWithFileContentEnv(t *testing.T, fileName string, wantError bool, wantConfig *Config) {
	t.Helper()
	b, err := bootstrapFileReadFunc(fileName)
	if err != nil {
		t.Skip(err)
	}
	origBootstrapContent := envconfig.XDSBootstrapFileContent
	envconfig.XDSBootstrapFileContent = string(b)
	defer func() { envconfig.XDSBootstrapFileContent = origBootstrapContent }()

	c, err := NewConfig()
	if (err != nil) != wantError {
		t.Fatalf("NewConfig() returned error %v, wantError: %v", err, wantError)
	}
	if wantError {
		return
	}
	if err := c.compare(wantConfig); err != nil {
		t.Fatal(err)
	}
}

// TestNewConfigV3ProtoFailure exercises the functionality in NewConfig with
// different bootstrap file contents which are expected to fail.
func TestNewConfigV3ProtoFailure(t *testing.T) {
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
			testNewConfigWithFileNameEnv(t, test.name, true, nil)
			testNewConfigWithFileContentEnv(t, test.name, true, nil)
		})
	}
}

// TestNewConfigV3ProtoSuccess exercises the functionality in NewConfig with
// different bootstrap file contents. It overrides the fileReadFunc by returning
// bootstrap file contents defined in this test, instead of reading from a file.
func TestNewConfigV3ProtoSuccess(t *testing.T) {
	cancel := setupBootstrapOverride(v3BootstrapFileMap)
	defer cancel()

	tests := []struct {
		name       string
		wantConfig *Config
	}{
		{
			"emptyNodeProto", &Config{
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "insecure"},
				},
				NodeProto: &v3corepb.Node{
					UserAgentName:        gRPCUserAgentName,
					UserAgentVersionType: &v3corepb.Node_UserAgentVersion{UserAgentVersion: grpc.Version},
					ClientFeatures:       []string{clientFeatureNoOverprovisioning, clientFeatureResourceWrapper},
				},
				ClientDefaultListenerResourceNameTemplate: "%s",
			},
		},
		{"unknownTopLevelFieldInFile", nilCredsConfigNoServerFeatures},
		{"unknownFieldInNodeProto", nilCredsConfigNoServerFeatures},
		{"unknownFieldInXdsServer", nilCredsConfigNoServerFeatures},
		{"multipleChannelCreds", nonNilCredsConfigV3},
		{"goodBootstrap", nonNilCredsConfigV3},
		{"multipleXDSServers", nonNilCredsConfigV3},
		{"serverSupportsIgnoreResourceDeletion", nonNilCredsConfigWithDeletionIgnored},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testNewConfigWithFileNameEnv(t, test.name, false, test.wantConfig)
			testNewConfigWithFileContentEnv(t, test.name, false, test.wantConfig)
		})
	}
}

// TestNewConfigBootstrapEnvPriority tests that the two env variables are read
// in correct priority.
//
// "GRPC_XDS_BOOTSTRAP" which specifies the file name containing the bootstrap
// configuration takes precedence over "GRPC_XDS_BOOTSTRAP_CONFIG", which
// directly specifies the bootstrap configuration in itself.
func TestNewConfigBootstrapEnvPriority(t *testing.T) {
	oldFileReadFunc := bootstrapFileReadFunc
	bootstrapFileReadFunc = func(filename string) ([]byte, error) {
		return fileReadFromFileMap(v3BootstrapFileMap, filename)
	}
	defer func() { bootstrapFileReadFunc = oldFileReadFunc }()

	goodFileName1 := "serverFeaturesIncludesXDSV3"
	goodConfig1 := nonNilCredsConfigV3

	goodFileName2 := "serverFeaturesExcludesXDSV3"
	goodFileContent2 := v3BootstrapFileMap[goodFileName2]
	goodConfig2 := nonNilCredsConfigNoServerFeatures

	origBootstrapFileName := envconfig.XDSBootstrapFileName
	envconfig.XDSBootstrapFileName = ""
	defer func() { envconfig.XDSBootstrapFileName = origBootstrapFileName }()

	origBootstrapContent := envconfig.XDSBootstrapFileContent
	envconfig.XDSBootstrapFileContent = ""
	defer func() { envconfig.XDSBootstrapFileContent = origBootstrapContent }()

	// When both env variables are empty, NewConfig should fail.
	if _, err := NewConfig(); err == nil {
		t.Errorf("NewConfig() returned nil error, expected to fail")
	}

	// When one of them is set, it should be used.
	envconfig.XDSBootstrapFileName = goodFileName1
	envconfig.XDSBootstrapFileContent = ""
	c, err := NewConfig()
	if err != nil {
		t.Errorf("NewConfig() failed: %v", err)
	}
	if err := c.compare(goodConfig1); err != nil {
		t.Error(err)
	}

	envconfig.XDSBootstrapFileName = ""
	envconfig.XDSBootstrapFileContent = goodFileContent2
	c, err = NewConfig()
	if err != nil {
		t.Errorf("NewConfig() failed: %v", err)
	}
	if err := c.compare(goodConfig2); err != nil {
		t.Error(err)
	}

	// Set both, file name should be read.
	envconfig.XDSBootstrapFileName = goodFileName1
	envconfig.XDSBootstrapFileContent = goodFileContent2
	c, err = NewConfig()
	if err != nil {
		t.Errorf("NewConfig() failed: %v", err)
	}
	if err := c.compare(goodConfig1); err != nil {
		t.Error(err)
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
		t.Fatalf("missing certprovider plugin %q", fakeCertProviderName)
	}
	wantCfg, err := parser.ParseConfig(json.RawMessage(`{"configKey": "configValue"}`))
	if err != nil {
		t.Fatalf("config parsing for plugin %q failed: %v", fakeCertProviderName, err)
	}

	cancel := setupBootstrapOverride(bootstrapFileMap)
	defer cancel()

	// Cannot use xdstestutils.ServerConfigForAddress here, as it would lead to
	// a cyclic dependency.
	jsonCfg := `{
		"server_uri": "trafficdirector.googleapis.com:443",
		"channel_creds": [{"type": "insecure"}],
		"server_features": ["xds_v3"]
	}`
	serverCfg, err := ServerConfigFromJSON([]byte(jsonCfg))
	if err != nil {
		t.Fatalf("Failed to create server config from JSON %s: %v", jsonCfg, err)
	}
	goodConfig := &Config{
		XDSServer: serverCfg,
		NodeProto: v3NodeProto,
		CertProviderConfigs: map[string]*certprovider.BuildableConfig{
			"fakeProviderInstance": wantCfg,
		},
		ClientDefaultListenerResourceNameTemplate: "%s",
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
			testNewConfigWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testNewConfigWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func TestNewConfigWithServerListenerResourceNameTemplate(t *testing.T) {
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
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "google_default"},
				},
				NodeProto:                                 v3NodeProto,
				ServerListenerResourceNameTemplate:        "grpc/server?xds.resource.listening_address=%s",
				ClientDefaultListenerResourceNameTemplate: "%s",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testNewConfigWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testNewConfigWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func TestNewConfigWithFederation(t *testing.T) {
	cancel := setupBootstrapOverride(map[string]string{
		"badClientListenerResourceNameTemplate": `
		{
			"node": { "id": "ENVOY_NODE_ID" },
			"xds_servers" : [{
				"server_uri": "trafficdirector.googleapis.com:443"
			}],
			"client_default_listener_resource_name_template": 123456789
		}`,
		"badClientListenerResourceNameTemplatePerAuthority": `
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
			name:    "badClientListenerResourceNameTemplate",
			wantErr: true,
		},
		{
			name:    "badClientListenerResourceNameTemplatePerAuthority",
			wantErr: true,
		},
		{
			name: "good",
			wantConfig: &Config{
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "google_default"},
				},
				NodeProto:                                 v3NodeProto,
				ServerListenerResourceNameTemplate:        "xdstp://xds.example.com/envoy.config.listener.v3.Listener/grpc/server?listening_address=%s",
				ClientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				Authorities: map[string]*Authority{
					"xds.td.com": {
						ClientListenerResourceNameTemplate: "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
						XDSServer: &ServerConfig{
							ServerURI:      "td.com",
							Creds:          ChannelCreds{Type: "google_default"},
							ServerFeatures: []string{"xds_v3"},
						},
					},
				},
			},
		},
		{
			name: "goodWithDefaultDefaultClientListenerTemplate",
			wantConfig: &Config{
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "google_default"},
				},
				NodeProto: v3NodeProto,
				ClientDefaultListenerResourceNameTemplate: "%s",
			},
		},
		{
			name: "goodWithDefaultClientListenerTemplatePerAuthority",
			wantConfig: &Config{
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "google_default"},
				},
				NodeProto: v3NodeProto,
				ClientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				Authorities: map[string]*Authority{
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
				XDSServer: &ServerConfig{
					ServerURI: "trafficdirector.googleapis.com:443",
					Creds:     ChannelCreds{Type: "google_default"},
				},
				NodeProto: v3NodeProto,
				ClientDefaultListenerResourceNameTemplate: "xdstp://xds.example.com/envoy.config.listener.v3.Listener/%s",
				Authorities: map[string]*Authority{
					"xds.td.com": {
						ClientListenerResourceNameTemplate: "xdstp://xds.td.com/envoy.config.listener.v3.Listener/%s",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testNewConfigWithFileNameEnv(t, test.name, test.wantErr, test.wantConfig)
			testNewConfigWithFileContentEnv(t, test.name, test.wantErr, test.wantConfig)
		})
	}
}

func TestServerConfigMarshalAndUnmarshal(t *testing.T) {
	jsonCfg := `{
		"server_uri": "test-server",
		"channel_creds": [{"type": "insecure"}],
		"server_features": ["xds_v3"]
	}`
	origConfig, err := ServerConfigFromJSON([]byte(jsonCfg))
	if err != nil {
		t.Fatalf("Failed to create server config from JSON %s: %v", jsonCfg, err)
	}
	bs, err := json.Marshal(origConfig)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	unmarshaledConfig := new(ServerConfig)
	if err := json.Unmarshal(bs, unmarshaledConfig); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if diff := cmp.Diff(origConfig, unmarshaledConfig); diff != "" {
		t.Fatalf("Unexpected diff in server config (-want, +got):\n%s", diff)
	}
}

func TestDefaultBundles(t *testing.T) {
	tests := []string{"google_default", "insecure", "tls"}

	for _, typename := range tests {
		t.Run(typename, func(t *testing.T) {
			if c := bootstrap.GetCredentials(typename); c == nil {
				t.Errorf(`bootstrap.GetCredentials(%s) credential is nil, want non-nil`, typename)
			}
		})
	}
}

func TestCredsBuilders(t *testing.T) {
	tests := []struct {
		typename string
		builder  bootstrap.Credentials
	}{
		{"google_default", &googleDefaultCredsBuilder{}},
		{"insecure", &insecureCredsBuilder{}},
		{"tls", &tlsCredsBuilder{}},
	}

	for _, test := range tests {
		t.Run(test.typename, func(t *testing.T) {
			if got, want := test.builder.Name(), test.typename; got != want {
				t.Errorf("%T.Name = %v, want %v", test.builder, got, want)
			}

			_, stop, err := test.builder.Build(nil)
			if err != nil {
				t.Fatalf("%T.Build failed: %v", test.builder, err)
			}
			stop()
		})
	}
}

func TestTlsCredsBuilder(t *testing.T) {
	tls := &tlsCredsBuilder{}
	_, stop, err := tls.Build(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tls.Build() failed with error %s when expected to succeed", err)
	}
	stop()

	if _, stop, err := tls.Build(json.RawMessage(`{"ca_certificate_file":"/ca_certificates.pem","refresh_interval": "asdf"}`)); err == nil {
		t.Errorf("tls.Build() succeeded with an invalid refresh interval, when expected to fail")
		stop()
	}
	// package internal/xdsclient/tlscreds has tests for config validity.
}
