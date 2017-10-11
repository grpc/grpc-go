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
	"fmt"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/test/leakcheck"
)

func TestParseLoadBalancer(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		scjs    string
		wantSC  ServiceConfig
		wantErr error
	}{
		{
			`{
      "loadBalancingPolicy":"round_robin",
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
			ServiceConfig{
				LB: newString("round_robin"),
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(true),
					},
				},
			},
			nil,
		},
		{
			`{
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
			ServiceConfig{},
			fmt.Errorf("json: cannot unmarshal number into Go struct field jsonSC.loadBalancingPolicy of type string"),
		},
	}

	for _, c := range testcases {
		sc, err := parseServiceConfig(c.scjs)
		if !errEqual(err, c.wantErr) || !reflect.DeepEqual(sc, c.wantSC) {
			t.Fatalf("parseServiceConfig(%s) = %+v, %v, want %+v, %v", c.scjs, sc, err, c.wantSC, c.wantErr)
		}
	}
}

func errEqual(a error, b error) bool {
	if a == nil && b == nil {
		return true
	}
	if a != nil && b != nil && a.Error() == b.Error() {
		return true
	}
	return false
}

func TestPraseWaitForReady(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		scjs    string
		wantSC  ServiceConfig
		wantErr error
	}{
		{
			`{
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
			ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(true),
					},
				},
			},
			nil,
		},
		{
			`{
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
			ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						WaitForReady: newBool(false),
					},
				},
			},
			nil,
		},
		{
			`{
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
			ServiceConfig{},
			fmt.Errorf("invalid character 'l' in literal false (expecting 's')"),
		},
	}

	for _, c := range testcases {
		sc, err := parseServiceConfig(c.scjs)
		if !errEqual(err, c.wantErr) || !reflect.DeepEqual(sc, c.wantSC) {
			t.Fatalf("parseServiceConfig(%s) = %+v, %v, want %+v, %v", c.scjs, sc, err, c.wantSC, c.wantErr)
		}
	}
}

func TestPraseTimeOut(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		scjs    string
		wantSC  ServiceConfig
		wantErr error
	}{
		{
			`{
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
			ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						Timeout: newDuration(time.Second),
					},
				},
			},
			nil,
		},
		{
			`{
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
			ServiceConfig{},
			fmt.Errorf("time: unknown unit c in duration 3c"),
		},
		{
			`{
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
			ServiceConfig{},
			fmt.Errorf("time: unknown unit c in duration 3c"),
		},
	}

	for _, c := range testcases {
		sc, err := parseServiceConfig(c.scjs)
		if !errEqual(err, c.wantErr) || !reflect.DeepEqual(sc, c.wantSC) {
			t.Fatalf("parseServiceConfig(%s) = %+v, %v, want %+v, %v", c.scjs, sc, err, c.wantSC, c.wantErr)
		}
	}
}

func TestPraseMsgSize(t *testing.T) {
	defer leakcheck.Check(t)
	testcases := []struct {
		scjs    string
		wantSC  ServiceConfig
		wantErr error
	}{
		{
			`{
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
			ServiceConfig{
				Methods: map[string]MethodConfig{
					"/foo/Bar": {
						MaxReqSize:  newInt(1024),
						MaxRespSize: newInt(2048),
					},
				},
			},
			nil,
		},
		{
			`{
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
        },
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
			ServiceConfig{},
			fmt.Errorf("json: cannot unmarshal string into Go struct field jsonMC.maxRequestMessageBytes of type int"),
		},
	}

	for _, c := range testcases {
		sc, err := parseServiceConfig(c.scjs)
		if !errEqual(err, c.wantErr) || !reflect.DeepEqual(sc, c.wantSC) {
			t.Fatalf("parseServiceConfig(%s) = %+v, %v, want %+v, %v", c.scjs, sc, err, c.wantSC, c.wantErr)
		}
	}
}
