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

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/golang/protobuf/jsonpb"
	durationpb "github.com/golang/protobuf/ptypes/duration"
	"github.com/google/go-cmp/cmp"

	configpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/meshca_experimental"
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
	goodConfigFullySpecified = &configpb.GoogleMeshCaConfig{
		Server: &v3corepb.ApiConfigSource{
			ApiType: v3corepb.ApiConfigSource_GRPC,
			GrpcServices: []*v3corepb.GrpcService{
				{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							TargetUri: "test-meshca",
							CallCredentials: []*v3corepb.GrpcService_GoogleGrpc_CallCredentials{
								// This call creds should be ignored.
								{
									CredentialSpecifier: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_AccessToken{},
								},
								{
									CredentialSpecifier: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService_{
										StsService: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService{
											TokenExchangeServiceUri: "http://test-sts",
											Resource:                "test-resource",
											Audience:                "test-audience",
											Scope:                   "test-scope",
											RequestedTokenType:      "test-requested-token-type",
											SubjectTokenPath:        "test-subject-token-path",
											SubjectTokenType:        "test-subject-token-type",
											ActorTokenPath:          "test-actor-token-path",
											ActorTokenType:          "test-actor-token-type",
										},
									},
								},
							},
						},
					},
					Timeout: &durationpb.Duration{Seconds: 10}, // 10s
				},
			},
		},
		CertificateLifetime: &durationpb.Duration{Seconds: 86400}, // 1d
		RenewalGracePeriod:  &durationpb.Duration{Seconds: 43200}, //12h
		KeyType:             configpb.GoogleMeshCaConfig_KEY_TYPE_RSA,
		KeySize:             uint32(2048),
		Location:            "us-west1-b",
	}
	goodConfigWithDefaults = &configpb.GoogleMeshCaConfig{
		Server: &v3corepb.ApiConfigSource{
			ApiType: v3corepb.ApiConfigSource_GRPC,
			GrpcServices: []*v3corepb.GrpcService{
				{
					TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
						GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
							CallCredentials: []*v3corepb.GrpcService_GoogleGrpc_CallCredentials{
								{
									CredentialSpecifier: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService_{
										StsService: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService{
											SubjectTokenPath: "test-subject-token-path",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
)

// makeJSONConfig marshals the provided config proto into JSON. This makes it
// possible for tests to specify the config in proto form, which is much easier
// than specifying the config in JSON form.
func makeJSONConfig(t *testing.T, cfg *configpb.GoogleMeshCaConfig) json.RawMessage {
	t.Helper()

	b := &bytes.Buffer{}
	m := &jsonpb.Marshaler{EnumsAsInts: true}
	if err := m.Marshal(b, cfg); err != nil {
		t.Fatalf("jsonpb.Marshal(%+v) failed: %v", cfg, err)
	}
	return json.RawMessage(b.Bytes())
}

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
	inputConfig := makeJSONConfig(t, goodConfigFullySpecified)
	wantConfig := "test-meshca:http://test-sts:test-resource:test-audience:test-scope:test-requested-token-type:test-subject-token-path:test-subject-token-type:test-actor-token-path:test-actor-token-type:10s:24h0m0s:12h0m0s:RSA:2048:us-west1-b"

	builder := newPluginBuilder()
	gotConfig, err := builder.ParseConfig(inputConfig)
	if err != nil {
		t.Fatalf("builder.ParseConfig(%q) failed: %v", inputConfig, err)
	}
	if diff := cmp.Diff(wantConfig, string(gotConfig.Canonical())); diff != "" {
		t.Errorf("builder.ParseConfig(%q) returned config does not match expected (-want +got):\n%s", inputConfig, diff)
	}
}

// TestParseConfigSuccessWithDefaults tests cases where the config is not fully
// specified, and we end up using some sane defaults.
func (s) TestParseConfigSuccessWithDefaults(t *testing.T) {
	inputConfig := makeJSONConfig(t, goodConfigWithDefaults)
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

	builder := newPluginBuilder()
	gotConfig, err := builder.ParseConfig(inputConfig)
	if err != nil {
		t.Fatalf("builder.ParseConfig(%q) failed: %v", inputConfig, err)

	}
	if diff := cmp.Diff(wantConfig, string(gotConfig.Canonical())); diff != "" {
		t.Errorf("builder.ParseConfig(%q) returned config does not match expected (-want +got):\n%s", inputConfig, diff)
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
		inputConfig interface{}
		wantErr     string
	}{
		{
			desc:        "bad config type",
			inputConfig: struct{ foo string }{foo: "bar"},
			wantErr:     "unsupported config type",
		},
		{
			desc:        "invalid JSON",
			inputConfig: json.RawMessage(`bad bad json`),
			wantErr:     "failed to unmarshal config",
		},
		{
			desc: "bad apiType",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_REST,
				},
			}),
			wantErr: "server has apiType REST, want GRPC",
		},
		{
			desc: "no grpc services",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_GRPC,
				},
			}),
			wantErr: "number of gRPC services in config is 0, expected 1",
		},
		{
			desc: "too many grpc services",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType:      v3corepb.ApiConfigSource_GRPC,
					GrpcServices: []*v3corepb.GrpcService{nil, nil},
				},
			}),
			wantErr: "number of gRPC services in config is 2, expected 1",
		},
		{
			desc: "missing google grpc service",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_GRPC,
					GrpcServices: []*v3corepb.GrpcService{
						{
							TargetSpecifier: &v3corepb.GrpcService_EnvoyGrpc_{
								EnvoyGrpc: &v3corepb.GrpcService_EnvoyGrpc{
									ClusterName: "foo",
								},
							},
						},
					},
				},
			}),
			wantErr: "missing google gRPC service in config",
		},
		{
			desc: "missing call credentials",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_GRPC,
					GrpcServices: []*v3corepb.GrpcService{
						{
							TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
									TargetUri: "foo",
								},
							},
						},
					},
				},
			}),
			wantErr: "missing call credentials in config",
		},
		{
			desc: "missing STS call credentials",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_GRPC,
					GrpcServices: []*v3corepb.GrpcService{
						{
							TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
									TargetUri: "foo",
									CallCredentials: []*v3corepb.GrpcService_GoogleGrpc_CallCredentials{
										{
											CredentialSpecifier: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_AccessToken{},
										},
									},
								},
							},
						},
					},
				},
			}),
			wantErr: "missing STS call credentials in config",
		},
		{
			desc: "with no defaults",
			inputConfig: makeJSONConfig(t, &configpb.GoogleMeshCaConfig{
				Server: &v3corepb.ApiConfigSource{
					ApiType: v3corepb.ApiConfigSource_GRPC,
					GrpcServices: []*v3corepb.GrpcService{
						{
							TargetSpecifier: &v3corepb.GrpcService_GoogleGrpc_{
								GoogleGrpc: &v3corepb.GrpcService_GoogleGrpc{
									CallCredentials: []*v3corepb.GrpcService_GoogleGrpc_CallCredentials{
										{
											CredentialSpecifier: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService_{
												StsService: &v3corepb.GrpcService_GoogleGrpc_CallCredentials_StsService{},
											},
										},
									},
								},
							},
						},
					},
				},
			}),
			wantErr: "missing subjectTokenPath in STS call credentials config",
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			builder := newPluginBuilder()
			sc, err := builder.ParseConfig(test.inputConfig)
			if err == nil {
				t.Fatalf("builder.ParseConfig(%q) = %v, expected to return error (%v)", test.inputConfig, string(sc.Canonical()), test.wantErr)

			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("builder.ParseConfig(%q) = (%v), want error (%v)", test.inputConfig, err, test.wantErr)
			}
		})
	}
}
