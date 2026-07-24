/*
 *
 * Copyright 2017 gRPC authors.
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

package grpc

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/serviceconfig"

	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
)

type parseTestCase struct {
	name    string
	scjs    string
	wantSC  *ServiceConfig
	wantErr bool
}

func lbConfigFor(t *testing.T, name string, cfg serviceconfig.LoadBalancingConfig) serviceconfig.LoadBalancingConfig {
	if name == "" {
		name = "pick_first"
		cfg = struct {
			serviceconfig.LoadBalancingConfig
		}{}
	}
	d := []map[string]any{{name: cfg}}
	strCfg, err := json.Marshal(d)
	t.Logf("strCfg = %v", string(strCfg))
	if err != nil {
		t.Fatalf("Error parsing config: %v", err)
	}
	parsedCfg, err := gracefulswitch.ParseConfig(strCfg)
	if err != nil {
		t.Fatalf("Error parsing config: %v", err)
	}
	return parsedCfg
}

func runParseTests(t *testing.T, testCases []parseTestCase) {
	t.Helper()
	for i, c := range testCases {
		name := c.name
		if name == "" {
			name = fmt.Sprint(i)
		}
		t.Run(name, func(t *testing.T) {
			scpr := parseServiceConfig(c.scjs, defaultMaxCallAttempts)
			var sc *ServiceConfig
			sc, _ = scpr.Config.(*ServiceConfig)
			if !c.wantErr {
				c.wantSC.rawJSONString = c.scjs
			}
			if c.wantErr != (scpr.Err != nil) || !reflect.DeepEqual(sc, c.wantSC) {
				t.Fatalf("parseServiceConfig(%s) = %+v, %v, want %+v, %v", c.scjs, sc, scpr.Err, c.wantSC, c.wantErr)
			}
		})
	}
}

type pbbData struct {
	serviceconfig.LoadBalancingConfig
	Foo string
	Bar int
}

type parseBalancerBuilder struct{}

func (parseBalancerBuilder) Name() string {
	return "pbb"
}

func (parseBalancerBuilder) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	d := pbbData{}
	if err := json.Unmarshal(c, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func (parseBalancerBuilder) Build(balancer.ClientConn, balancer.BuildOptions) balancer.Balancer {
	panic("unimplemented")
}

func init() {
	balancer.Register(parseBalancerBuilder{})
}

func (s) TestParseLBConfig(t *testing.T) {
	testcases := []parseTestCase{
		{
			scjs: `{
    "loadBalancingConfig": [{"pbb": { "foo": "hi" } }]
}`,
			wantSC: &ServiceConfig{
				Methods:  make(map[string]MethodConfig),
				lbConfig: lbConfigFor(t, "pbb", pbbData{Foo: "hi"}),
			},
			wantErr: false,
		},
	}
	runParseTests(t, testcases)
}

func (s) TestParseNoLBConfigSupported(t *testing.T) {
	// We have a loadBalancingConfig field but will not encounter a supported
	// policy.  The config will be considered invalid in this case.
	testcases := []parseTestCase{
		{
			scjs: `{
    "loadBalancingConfig": [{"not_a_balancer1": {} }, {"not_a_balancer2": {}}]
}`,
			wantErr: true,
		}, {
			scjs:    `{"loadBalancingConfig": []}`,
			wantErr: true,
		},
	}
	runParseTests(t, testcases)
}

func (s) TestParseLoadBalancer(t *testing.T) {
	testcases := []parseTestCase{
		{
			scjs: `{
    "loadBalancingPolicy": "round_robin",
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": true
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(true),
					},
				},
				lbConfig: lbConfigFor(t, "round_robin", nil),
			},
			wantErr: false,
		},
		{
			scjs: `{
    "loadBalancingPolicy": 1,
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": false
        }
    ]
}`,
			wantErr: true,
		},
	}
	runParseTests(t, testcases)
}

func (s) TestParseWaitForReady(t *testing.T) {
	testcases := []parseTestCase{
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": true
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(true),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": false
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(false),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": fall
        },
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "waitForReady": true
        }
    ]
}`,
			wantErr: true,
		},
	}

	runParseTests(t, testcases)
}

func (s) TestParseTimeOut(t *testing.T) {
	testcases := []parseTestCase{
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "timeout": "1s"
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						Timeout: newDuration(time.Second),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "timeout": "3c"
        }
    ]
}`,
			wantErr: true,
		},
		{
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "timeout": "3c"
        },
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "timeout": "1s"
        }
    ]
}`,
			wantErr: true,
		},
	}

	runParseTests(t, testcases)
}

func (s) TestJSONUint32(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    jsonUint32
		wantErr bool
	}{
		{
			name:  "number",
			input: `42`,
			want:  42,
		},
		{
			name:  "string",
			input: `"42"`,
			want:  42,
		},
		{
			name:  "uint32 maximum",
			input: `"4294967295"`,
			want:  4294967295,
		},
		{
			name:  "null",
			input: `null`,
			want:  0,
		},
		{
			name:  "fractional leading zeros with exponent",
			input: `"0.00000000001e11"`,
			want:  1,
		},
		{
			name:  "negative zero",
			input: `"-0"`,
			want:  0,
		},
		{
			name:  "zero with oversized exponent",
			input: `"0e999999999999999999999"`,
			want:  0,
		},
		{
			name:    "oversized exponent",
			input:   `"1e999999999999999999999"`,
			wantErr: true,
		},
		{
			name:  "quoted exponent",
			input: `"1e2"`,
			want:  100,
		},
		{
			name:  "quoted zero fraction",
			input: `"1.0"`,
			want:  1,
		},
		{
			name:    "negative",
			input:   `"-1"`,
			wantErr: true,
		},
		{
			name:    "uint32 overflow",
			input:   `"4294967296"`,
			wantErr: true,
		},
		{
			name:    "nonzero fraction",
			input:   `"1.5"`,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   `""`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got jsonUint32
			err := json.Unmarshal([]byte(test.input), &got)
			if (err != nil) != test.wantErr {
				t.Fatalf("json.Unmarshal(%q) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("json.Unmarshal(%q) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func (s) TestParseMsgSize(t *testing.T) {
	testcases := []parseTestCase{
		{
			name: "numeric integers",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "maxRequestMessageBytes": 1024,
            "maxResponseMessageBytes": 2048
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						MaxReqSize:  newInt(1024),
						MaxRespSize: newInt(2048),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			name: "stringified integers",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "maxRequestMessageBytes": "1024",
            "maxResponseMessageBytes": "2048"
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						MaxReqSize:  newInt(1024),
						MaxRespSize: newInt(2048),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			name: "invalid stringified integer",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "maxRequestMessageBytes": "1024.5"
        }
    ]
}`,
			wantErr: true,
		},
	}

	runParseTests(t, testcases)
}

func (s) TestParseRetryMaxAttempts(t *testing.T) {
	retryPolicy := func() *internalserviceconfig.RetryPolicy {
		return &internalserviceconfig.RetryPolicy{
			MaxAttempts:       4,
			InitialBackoff:    time.Second,
			MaxBackoff:        2 * time.Second,
			BackoffMultiplier: 2,
			RetryableStatusCodes: map[codes.Code]bool{
				codes.Unavailable: true,
			},
		}
	}

	testcases := []parseTestCase{
		{
			name: "numeric integer",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "retryPolicy": {
                "maxAttempts": 4,
                "initialBackoff": "1s",
                "maxBackoff": "2s",
                "backoffMultiplier": 2,
                "retryableStatusCodes": ["UNAVAILABLE"]
            }
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						RetryPolicy: retryPolicy(),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			name: "stringified integer",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "retryPolicy": {
                "maxAttempts": "4",
                "initialBackoff": "1s",
                "maxBackoff": "2s",
                "backoffMultiplier": 2,
                "retryableStatusCodes": ["UNAVAILABLE"]
            }
        }
    ]
}`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						RetryPolicy: retryPolicy(),
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			name: "invalid stringified integer",
			scjs: `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "foo",
                    "method": "Bar"
                }
            ],
            "retryPolicy": {
                "maxAttempts": "4.5",
                "initialBackoff": "1s",
                "maxBackoff": "2s",
                "backoffMultiplier": 2,
                "retryableStatusCodes": ["UNAVAILABLE"]
            }
        }
    ]
}`,
			wantErr: true,
		},
	}

	runParseTests(t, testcases)
}

func (s) TestParseDefaultMethodConfig(t *testing.T) {
	dc := &ServiceConfig{
		Methods: map[string]MethodConfig{
			"": {WaitForReady: newBool(true)},
		},
		lbConfig: lbConfigFor(t, "", nil),
	}

	runParseTests(t, []parseTestCase{
		{
			scjs: `{
  "methodConfig": [{
    "name": [{}],
    "waitForReady": true
  }]
}`,
			wantSC: dc,
		},
		{
			scjs: `{
  "methodConfig": [{
    "name": [{"service": null}],
    "waitForReady": true
  }]
}`,
			wantSC: dc,
		},
		{
			scjs: `{
  "methodConfig": [{
    "name": [{"service": ""}],
    "waitForReady": true
  }]
}`,
			wantSC: dc,
		},
		{
			scjs: `{
  "methodConfig": [{
    "name": [{"method": "Bar"}],
    "waitForReady": true
  }]
}`,
			wantErr: true,
		},
		{
			scjs: `{
  "methodConfig": [{
    "name": [{"service": "", "method": "Bar"}],
    "waitForReady": true
  }]
}`,
			wantErr: true,
		},
	})
}

func (s) TestParseMethodConfigDuplicatedName(t *testing.T) {
	runParseTests(t, []parseTestCase{
		{
			scjs: `{
  "methodConfig": [{
    "name": [
      {"service": "foo"},
      {"service": "foo"}
    ],
    "waitForReady": true
  }]
}`,
			wantErr: true,
		},
	})
}

func (s) TestParseRetryPolicy(t *testing.T) {
	runParseTests(t, []parseTestCase{
		{
			name: "valid",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					"maxAttempts": 2,
					"initialBackoff": "2s",
					"maxBackoff": "10s",
					"backoffMultiplier": 2,
					"retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantSC: &ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/": {
						RetryPolicy: &internalserviceconfig.RetryPolicy{
							MaxAttempts:          2,
							InitialBackoff:       2 * time.Second,
							MaxBackoff:           10 * time.Second,
							BackoffMultiplier:    2,
							RetryableStatusCodes: map[codes.Code]bool{codes.Unavailable: true},
						},
					},
				},
				lbConfig: lbConfigFor(t, "", nil),
			},
		},
		{
			name: "negative maxAttempts",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "maxAttempts": -1,
					  "initialBackoff": "2s",
					  "maxBackoff": "10s",
					  "backoffMultiplier": 2,
					  "retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantErr: true,
		},
		{
			name: "missing maxAttempts",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "initialBackoff": "2s",
					  "maxBackoff": "10s",
					  "backoffMultiplier": 2,
					  "retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantErr: true,
		},
		{
			name: "zero initialBackoff",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "maxAttempts": 2,
					  "initialBackoff": "0s",
					  "maxBackoff": "10s",
					  "backoffMultiplier": 2,
					  "retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantErr: true,
		},
		{
			name: "zero maxBackoff",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "maxAttempts": 2,
					  "initialBackoff": "2s",
					  "maxBackoff": "0s",
					  "backoffMultiplier": 2,
					  "retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantErr: true,
		},
		{
			name: "zero backoffMultiplier",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "maxAttempts": 2,
					  "initialBackoff": "2s",
					  "maxBackoff": "10s",
					  "backoffMultiplier": 0,
					  "retryableStatusCodes": ["UNAVAILABLE"]
				  }
				}]
			  }`,
			wantErr: true,
		},
		{
			name: "no retryable codes",
			scjs: `{
				"methodConfig": [{
				  "name": [{"service": "foo"}],
				  "retryPolicy": {
					  "maxAttempts": 2,
					  "initialBackoff": "2s",
					  "maxBackoff": "10s",
					  "backoffMultiplier": 2,
					  "retryableStatusCodes": []
				  }
				}]
			  }`,
			wantErr: true,
		},
	})
}

func newBool(b bool) *bool {
	return &b
}

func newDuration(b time.Duration) *time.Duration {
	return &b
}
