/*
 *
 * Copyright 2020 gRPC authors.
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

package pemfile

import (
	"encoding/json"
	"testing"
)

type testConfig struct {
	CertificateFile   string `json:"certificate_file,omitempty"`
	PrivateKeyFile    string `json:"private_key_file,omitempty"`
	CACertificateFile string `json:"ca_certificate_file,omitempty"`
	RefreshInterval   string `json:"refresh_interval,omitempty"`
}

func makeJSONConfig(t *testing.T, cfg testConfig) json.RawMessage {
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal(%+v) failed: %v", cfg, err)
	}
	return json.RawMessage(b)
}

func TestParseConfig(t *testing.T) {
	tests := []struct {
		desc       string
		input      interface{}
		wantOutput string
		wantErr    bool
	}{
		{
			desc:    "non JSON input",
			input:   &testConfig{},
			wantErr: true,
		},
		{
			desc:    "invalid JSON",
			input:   json.RawMessage(`bad bad json`),
			wantErr: true,
		},
		{
			desc:    "JSON input does not match expected",
			input:   json.RawMessage(`["foo": "bar"]`),
			wantErr: true,
		},
		{
			desc:    "no credential files",
			input:   makeJSONConfig(t, testConfig{}),
			wantErr: true,
		},
		{
			desc: "only cert file",
			input: makeJSONConfig(t, testConfig{
				CertificateFile: "/a/b/cert.pem",
			}),
			wantErr: true,
		},
		{
			desc: "only key file",
			input: makeJSONConfig(t, testConfig{
				PrivateKeyFile: "/a/b/key.pem",
			}),
			wantErr: true,
		},
		{
			desc: "cert and key in different directories",
			input: makeJSONConfig(t, testConfig{
				CertificateFile: "/b/a/cert.pem",
				PrivateKeyFile:  "/a/b/key.pem",
			}),
			wantErr: true,
		},
		{
			desc: "bad refresh duration",
			input: makeJSONConfig(t, testConfig{
				CertificateFile:   "/a/b/cert.pem",
				PrivateKeyFile:    "/a/b/key.pem",
				CACertificateFile: "/a/b/ca.pem",
				RefreshInterval:   "duration",
			}),
			wantErr: true,
		},
		{
			desc: "good config with default refresh interval",
			input: makeJSONConfig(t, testConfig{
				CertificateFile:   "/a/b/cert.pem",
				PrivateKeyFile:    "/a/b/key.pem",
				CACertificateFile: "/a/b/ca.pem",
			}),
			wantOutput: "file_watcher:/a/b/cert.pem:/a/b/key.pem:/a/b/ca.pem:10m0s",
		},
		{
			desc: "good config",
			input: makeJSONConfig(t, testConfig{
				CertificateFile:   "/a/b/cert.pem",
				PrivateKeyFile:    "/a/b/key.pem",
				CACertificateFile: "/a/b/ca.pem",
				RefreshInterval:   "200s",
			}),
			wantOutput: "file_watcher:/a/b/cert.pem:/a/b/key.pem:/a/b/ca.pem:3m20s",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			builder := &pluginBuilder{}

			bc, err := builder.ParseConfig(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseConfig(%+v) failed: %v", test.input, err)
			}
			if test.wantErr {
				return
			}

			gotConfig := bc.String()
			if gotConfig != test.wantOutput {
				t.Fatalf("ParseConfig(%v) = %s, want %s", test.input, gotConfig, test.wantOutput)
			}
		})
	}
}
