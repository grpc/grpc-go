/*
 *
 * Copyright 2023 gRPC authors.
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

package dns_test

import (
	"context"
	"net"
	"sync"

	"google.golang.org/grpc/internal/testutils"
)

// A fake implementation of the internal.NetResolver interface for use in tests.
type testNetResolver struct {
	// A write to this channel is made when this resolver receives a resolution
	// request. Tests can rely on reading from this channel to be notified about
	// resolution requests instead of sleeping for a predefined period of time.
	lookupHostCh *testutils.Channel

	mu              sync.Mutex
	hostLookupTable map[string][]string
}

func (tr *testNetResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	if tr.lookupHostCh != nil {
		tr.lookupHostCh.Send(nil)
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()

	if addrs, ok := tr.hostLookupTable[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{
		Err:         "hostLookup error",
		Name:        host,
		Server:      "fake",
		IsTemporary: true,
	}
}

func (tr *testNetResolver) UpdateHostLookupTable(table map[string][]string) {
	tr.mu.Lock()
	tr.hostLookupTable = table
	tr.mu.Unlock()
}

func (*testNetResolver) LookupSRV(ctx context.Context, service, proto, name string) (string, []*net.SRV, error) {
	return srvLookup(service, proto, name)
}

func (*testNetResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	return txtLookup(host)
}

var srvLookupTbl = struct {
	sync.Mutex
	tbl map[string][]*net.SRV
}{
	tbl: map[string][]*net.SRV{
		"_grpclb._tcp.srv.ipv4.single.fake": {&net.SRV{Target: "ipv4.single.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv4.multi.fake":  {&net.SRV{Target: "ipv4.multi.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv6.single.fake": {&net.SRV{Target: "ipv6.single.fake", Port: 1234}},
		"_grpclb._tcp.srv.ipv6.multi.fake":  {&net.SRV{Target: "ipv6.multi.fake", Port: 1234}},
	},
}

func srvLookup(service, proto, name string) (string, []*net.SRV, error) {
	cname := "_" + service + "._" + proto + "." + name
	srvLookupTbl.Lock()
	defer srvLookupTbl.Unlock()
	if srvs, cnt := srvLookupTbl.tbl[cname]; cnt {
		return cname, srvs, nil
	}
	return "", nil, &net.DNSError{
		Err:         "srvLookup error",
		Name:        cname,
		Server:      "fake",
		IsTemporary: true,
	}
}

// scfs contains an array of service config file string in JSON format.
// Notes about the scfs contents and usage:
// scfs contains 4 service config file JSON strings for testing. Inside each
// service config file, there are multiple choices. scfs[0:3] each contains 5
// choices, and first 3 choices are nonmatching choices based on canarying rule,
// while the last two are matched choices. scfs[3] only contains 3 choices, and
// all of them are nonmatching based on canarying rule. For each of scfs[0:3],
// the eventually returned service config, which is from the first of the two
// matched choices, is stored in the corresponding scs element (e.g.
// scfs[0]->scs[0]). scfs and scs elements are used in pair to test the dns
// resolver functionality, with scfs as the input and scs used for validation of
// the output. For scfs[3], it corresponds to empty service config, since there
// isn't a matched choice.
var scfs = []string{
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true,
					"timeout": "1s",
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientLanguage": [
			"GO"
		],
		"percentage": 100,
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo"
						}
					],
					"waitForReady": true,
					"timeout": "1s"
				},
				{
					"name": [
						{
							"service": "bar"
						}
					],
					"waitForReady": false
				}
			]
		}
	},
	{
		"serviceConfig": {
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true
				}
			]
		}
	}
]`,
	`[
	{
		"clientLanguage": [
			"CPP",
			"JAVA"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"percentage": 0,
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	},
	{
		"clientHostName": [
			"localhost"
		],
		"serviceConfig": {
			"loadBalancingPolicy": "grpclb",
			"methodConfig": [
				{
					"name": [
						{
							"service": "all"
						}
					],
					"timeout": "1s"
				}
			]
		}
	}
]`,
}

// scs contains an array of service config string in JSON format.
var scs = []string{
	`{
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}`,
	`{
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo",
							"method": "bar"
						}
					],
					"waitForReady": true,
					"timeout": "1s",
					"maxRequestMessageBytes": 1024,
					"maxResponseMessageBytes": 1024
				}
			]
		}`,
	`{
			"loadBalancingPolicy": "round_robin",
			"methodConfig": [
				{
					"name": [
						{
							"service": "foo"
						}
					],
					"waitForReady": true,
					"timeout": "1s"
				},
				{
					"name": [
						{
							"service": "bar"
						}
					],
					"waitForReady": false
				}
			]
		}`,
}

// scLookupTbl is a map, which contains targets that have service config to
// their configs.  Targets not in this set should not have service config.
var scLookupTbl = map[string]string{
	"foo.bar.com":          scs[0],
	"srv.ipv4.single.fake": scs[1],
	"srv.ipv4.multi.fake":  scs[2],
}

// generateSC returns a service config string in JSON format for the input name.
func generateSC(name string) string {
	return scLookupTbl[name]
}

// generateSCF generates a slice of strings (aggregately representing a single
// service config file) for the input config string, which mocks the result
// from a real DNS TXT record lookup.
func generateSCF(cfg string) []string {
	// In DNS, service config is encoded in a TXT record via the mechanism
	// described in RFC-1464 using the attribute name grpc_config.
	b := append([]byte("grpc_config="), []byte(cfg)...)

	// Split b into multiple strings, each with a max of 255 bytes, which is
	// the DNS TXT record limit.
	var r []string
	for i := 0; i < len(b); i += txtBytesLimit {
		if i+txtBytesLimit > len(b) {
			r = append(r, string(b[i:]))
		} else {
			r = append(r, string(b[i:i+txtBytesLimit]))
		}
	}
	return r
}

var txtLookupTbl = struct {
	sync.Mutex
	tbl map[string][]string
}{
	tbl: map[string][]string{
		"_grpc_config.foo.bar.com":          generateSCF(scfs[0]),
		"_grpc_config.srv.ipv4.single.fake": generateSCF(scfs[1]),
		"_grpc_config.srv.ipv4.multi.fake":  generateSCF(scfs[2]),
		"_grpc_config.srv.ipv6.single.fake": generateSCF(scfs[3]),
		"_grpc_config.srv.ipv6.multi.fake":  generateSCF(scfs[3]),
	},
}

func txtLookup(host string) ([]string, error) {
	txtLookupTbl.Lock()
	defer txtLookupTbl.Unlock()
	if scs, cnt := txtLookupTbl.tbl[host]; cnt {
		return scs, nil
	}
	return nil, &net.DNSError{
		Err:         "txtLookup error",
		Name:        host,
		Server:      "fake",
		IsTemporary: true,
	}
}
