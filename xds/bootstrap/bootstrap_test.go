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
 *
 */
package bootstrap

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/internal/envconfig"
	"google.golang.org/grpc/internal/testutils"
)

const testCredsBuilderName = "test_creds"

var builder = &testCredsBuilder{}

func init() {
	RegisterChannelCredentials(builder)
}

type testCredsBuilder struct {
	config json.RawMessage
}

func (t *testCredsBuilder) Build(config json.RawMessage) (credentials.Bundle, func(), error) {
	t.config = config
	return nil, nil, nil
}

func (t *testCredsBuilder) Name() string {
	return testCredsBuilderName
}

func TestRegisterNew(t *testing.T) {
	c := GetChannelCredentials(testCredsBuilderName)
	if c == nil {
		t.Fatalf("GetCredentials(%q) credential = nil", testCredsBuilderName)
	}

	const sampleConfig = "sample_config"
	rawMessage := json.RawMessage(sampleConfig)
	if _, _, err := c.Build(rawMessage); err != nil {
		t.Errorf("Build(%v) error = %v, want nil", rawMessage, err)
	}

	if got, want := string(builder.config), sampleConfig; got != want {
		t.Errorf("Build config = %v, want %v", got, want)
	}
}

func TestChannelCredsBuilders(t *testing.T) {
	tests := []struct {
		typename              string
		builder               ChannelCredentials
		minimumRequiredConfig json.RawMessage
	}{
		{"google_default", &googleDefaultCredsBuilder{}, nil},
		{"insecure", &insecureCredsBuilder{}, nil},
		{"tls", &tlsCredsBuilder{}, nil},
	}

	for _, test := range tests {
		t.Run(test.typename, func(t *testing.T) {
			if got, want := test.builder.Name(), test.typename; got != want {
				t.Errorf("%T.Name = %v, want %v", test.builder, got, want)
			}

			bundle, stop, err := test.builder.Build(test.minimumRequiredConfig)
			if err != nil {
				t.Fatalf("%T.Build failed: %v", test.builder, err)
			}
			if bundle == nil {
				t.Errorf("%T.Build returned nil bundle, expected non-nil", test.builder)
			}
			stop()
		})
	}
}

func TestCallCredsBuilders(t *testing.T) {
	tests := []struct {
		typename              string
		builder               CallCredentials
		minimumRequiredConfig json.RawMessage
	}{
		{"jwt_token_file", &jwtCallCredsBuilder{}, json.RawMessage(`{"jwt_token_file":"/path/to/token.jwt"}`)},
	}

	for _, test := range tests {
		t.Run(test.typename, func(t *testing.T) {
			if got, want := test.builder.Name(), test.typename; got != want {
				t.Errorf("%T.Name = %v, want %v", test.builder, got, want)
			}

			bundle, stop, err := test.builder.Build(test.minimumRequiredConfig)
			if err != nil {
				t.Fatalf("%T.Build failed: %v", test.builder, err)
			}
			if bundle == nil {
				t.Errorf("%T.Build returned nil bundle, expected non-nil", test.builder)
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
}

func TestJwtCallCredentials_DisabledIfFeatureNotEnabled(t *testing.T) {
	builder := GetCallCredentials("jwt_call_creds")
	if builder != nil {
		t.Fatal("Expected nil Credentials for jwt_call_creds when the feature is disabled.")
	}

	testutils.SetEnvConfig(t, &envconfig.XDSBootstrapCallCredsEnabled, true)

	// Test that GetCredentials returns the JWT builder.
	builder = GetCallCredentials("jwt_token_file")
	if builder == nil {
		t.Fatal("GetCallCredentials(\"jwt_token_file\") returned nil")
	}
	if got, want := builder.Name(), "jwt_token_file"; got != want {
		t.Errorf("Retrieved builder name = %q, want %q", got, want)
	}
}
