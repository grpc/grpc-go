// +build go1.13

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

package meshca

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
)

const (
	testProjectID  = "test-project-id"
	testGKECluster = "test-gke-cluster"
	testGCEZone    = "test-zone"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var (
	goodConfigFormatStr = `
	{
		"server": {
			"api_type": 2,
			"grpc_services": [
				{
					"googleGrpc": {
						"target_uri": %q,
						"call_credentials": [
							{
								"access_token": "foo"
							},
							{
								"sts_service": {
									"token_exchange_service_uri": "http://test-sts",
									"resource": "test-resource",
									"audience": "test-audience",
									"scope": "test-scope",
									"requested_token_type": "test-requested-token-type",
									"subject_token_path": "test-subject-token-path",
									"subject_token_type": "test-subject-token-type",
									"actor_token_path": "test-actor-token-path",
									"actor_token_type": "test-actor-token-type"
								}
							}
						]
					},
					"timeout": "10s"
				}
			]
		},
		"certificate_lifetime": "86400s",
		"renewal_grace_period":  "43200s",
		"key_type": 1,
		"key_size": 2048,
		"location": "us-west1-b"
	}`
	goodConfigWithDefaults = json.RawMessage(`
	{
		"server": {
			"api_type": 2,
			"grpc_services": [
				{
					"googleGrpc": {
						"call_credentials": [
							{
								"sts_service": {
									"subject_token_path": "test-subject-token-path"
								}
							}
						]
					},
					"timeout": "10s"
				}
			]
		}
	}`)
)

var goodConfigFullySpecified = json.RawMessage(fmt.Sprintf(goodConfigFormatStr, "test-meshca"))

// verifyReceivedRequest reads the HTTP request received by the fake client
// (exposed through a channel), and verifies that it matches the expected
// request.
func verifyReceivedRequest(fc *testutils.FakeHTTPClient, wantURI string) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	val, err := fc.ReqChan.Receive(ctx)
	if err != nil {
		return err
	}
	gotReq := val.(*http.Request)
	if gotURI := gotReq.URL.String(); gotURI != wantURI {
		return fmt.Errorf("request contains URL %q want %q", gotURI, wantURI)
	}
	if got, want := gotReq.Header.Get("Metadata-Flavor"), "Google"; got != want {
		return fmt.Errorf("request contains flavor %q want %q", got, want)
	}
	return nil
}

// TestParseConfigSuccessFullySpecified tests the case where the config is fully
// specified and no defaults are required.
func (s) TestParseConfigSuccessFullySpecified(t *testing.T) {
	wantConfig := "test-meshca:http://test-sts:test-resource:test-audience:test-scope:test-requested-token-type:test-subject-token-path:test-subject-token-type:test-actor-token-path:test-actor-token-type:10s:24h0m0s:12h0m0s:RSA:2048:us-west1-b"

	cfg, err := pluginConfigFromJSON(goodConfigFullySpecified)
	if err != nil {
		t.Fatalf("pluginConfigFromJSON(%q) failed: %v", goodConfigFullySpecified, err)
	}
	gotConfig := cfg.canonical()
	if diff := cmp.Diff(wantConfig, string(gotConfig)); diff != "" {
		t.Errorf("pluginConfigFromJSON(%q) returned config does not match expected (-want +got):\n%s", string(goodConfigFullySpecified), diff)
	}
}

// TestParseConfigSuccessWithDefaults tests cases where the config is not fully
// specified, and we end up using some sane defaults.
func (s) TestParseConfigSuccessWithDefaults(t *testing.T) {
	wantConfig := fmt.Sprintf("%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s:%s",
		"meshca.googleapis.com",      // Mesh CA Server URI.
		"securetoken.googleapis.com", // STS Server URI.
		"",                           // STS Resource Name.
		"identitynamespace:test-project-id.svc.id.goog:https://container.googleapis.com/v1/projects/test-project-id/zones/test-zone/clusters/test-gke-cluster", // STS Audience.
		"https://www.googleapis.com/auth/cloud-platform", // STS Scope.
		"urn:ietf:params:oauth:token-type:access_token",  // STS requested token type.
		"test-subject-token-path",                        // STS subject token path.
		"urn:ietf:params:oauth:token-type:jwt",           // STS subject token type.
		"",                                               // STS actor token path.
		"",                                               // STS actor token type.
		"10s",                                            // Call timeout.
		"24h0m0s",                                        // Cert life time.
		"12h0m0s",                                        // Cert grace time.
		"RSA",                                            // Key type
		"2048",                                           // Key size
		"test-zone",                                      // Zone
	)

	// We expect the config parser to make four HTTP requests and receive four
	// responses. Hence we setup the request and response channels in the fake
	// client with appropriate buffer size.
	fc := &testutils.FakeHTTPClient{
		ReqChan:  testutils.NewChannelWithSize(4),
		RespChan: testutils.NewChannelWithSize(4),
	}
	// Set up the responses to be delivered to the config parser by the fake
	// client. The config parser expects responses with project_id,
	// gke_cluster_id and gce_zone. The zone is read twice, once as part of
	// reading the STS audience and once to get location metadata.
	fc.RespChan.Send(&http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte(testProjectID))),
	})
	fc.RespChan.Send(&http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte(testGKECluster))),
	})
	fc.RespChan.Send(&http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte(fmt.Sprintf("projects/%s/zones/%s", testProjectID, testGCEZone)))),
	})
	fc.RespChan.Send(&http.Response{
		Status:     "200 OK",
		StatusCode: http.StatusOK,
		Body:       ioutil.NopCloser(bytes.NewReader([]byte(fmt.Sprintf("projects/%s/zones/%s", testProjectID, testGCEZone)))),
	})
	// Override the http.Client with our fakeClient.
	origMakeHTTPDoer := makeHTTPDoer
	makeHTTPDoer = func() httpDoer { return fc }
	defer func() { makeHTTPDoer = origMakeHTTPDoer }()

	// Spawn a goroutine to verify the HTTP requests sent out as part of the
	// config parsing.
	errCh := make(chan error, 1)
	go func() {
		if err := verifyReceivedRequest(fc, "http://metadata.google.internal/computeMetadata/v1/project/project-id"); err != nil {
			errCh <- err
			return
		}
		if err := verifyReceivedRequest(fc, "http://metadata.google.internal/computeMetadata/v1/instance/attributes/cluster-name"); err != nil {
			errCh <- err
			return
		}
		if err := verifyReceivedRequest(fc, "http://metadata.google.internal/computeMetadata/v1/instance/zone"); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	cfg, err := pluginConfigFromJSON(goodConfigWithDefaults)
	if err != nil {
		t.Fatalf("pluginConfigFromJSON(%q) failed: %v", goodConfigWithDefaults, err)
	}
	gotConfig := cfg.canonical()
	if diff := cmp.Diff(wantConfig, string(gotConfig)); diff != "" {
		t.Errorf("builder.ParseConfig(%q) returned config does not match expected (-want +got):\n%s", goodConfigWithDefaults, diff)
	}

	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

// TestParseConfigFailureCases tests several invalid configs which all result in
// config parsing failures.
func (s) TestParseConfigFailureCases(t *testing.T) {
	tests := []struct {
		desc        string
		inputConfig json.RawMessage
		wantErr     string
	}{
		{
			desc:        "invalid JSON",
			inputConfig: json.RawMessage(`bad bad json`),
			wantErr:     "failed to unmarshal config",
		},
		{
			desc: "bad apiType",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 1
				}
			}`),
			wantErr: "server has apiType REST, want GRPC",
		},
		{
			desc: "no grpc services",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2
				}
			}`),
			wantErr: "number of gRPC services in config is 0, expected 1",
		},
		{
			desc: "too many grpc services",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2,
					"grpc_services": [{}, {}]
				}
			}`),
			wantErr: "number of gRPC services in config is 2, expected 1",
		},
		{
			desc: "missing google grpc service",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2,
					"grpc_services": [
						{
							"envoyGrpc": {}
						}
					]
				}
			}`),
			wantErr: "missing google gRPC service in config",
		},
		{
			desc: "missing call credentials",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2,
					"grpc_services": [
						{
							"googleGrpc": {
								"target_uri": "foo"
							}
						}
					]
				}
			}`),
			wantErr: "missing call credentials in config",
		},
		{
			desc: "missing STS call credentials",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2,
					"grpc_services": [
						{
							"googleGrpc": {
								"target_uri": "foo",
								"call_credentials": [
									{
										"access_token": "foo"
									}
								]
							}
						}
					]
				}
			}`),
			wantErr: "missing STS call credentials in config",
		},
		{
			desc: "with no defaults",
			inputConfig: json.RawMessage(`
			{
				"server": {
					"api_type": 2,
					"grpc_services": [
						{
							"googleGrpc": {
								"target_uri": "foo",
								"call_credentials": [
									{
										"sts_service": {}
									}
								]
							}
						}
					]
				}
			}`),
			wantErr: "missing subjectTokenPath in STS call credentials config",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			cfg, err := pluginConfigFromJSON(test.inputConfig)
			if err == nil {
				t.Fatalf("pluginConfigFromJSON(%q) = %v, expected to return error (%v)", test.inputConfig, string(cfg.canonical()), test.wantErr)

			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("builder.ParseConfig(%q) = (%v), want error (%v)", test.inputConfig, err, test.wantErr)
			}
		})
	}
}
