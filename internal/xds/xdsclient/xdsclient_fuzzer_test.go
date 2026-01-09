/*
 *
 * Copyright 2026 gRPC authors.
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

package xdsclient

import (
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/internal/xds/bootstrap"
	xdsclient "google.golang.org/grpc/internal/xds/clients/xdsclient"
	"google.golang.org/protobuf/proto"
)

// Fuzzer holds the state for the fuzzer, including the xDS client under test,
// fake transport and active watchers.
type Fuzzer struct {
	xdsClient        *xdsclient.XDSClient
	transportBuilder *FakeTransportBuilder
	activeWatches    map[string]func()
}

func NewFuzzer(t *testing.T, bootstrapConfig string) (*Fuzzer, error) {
	config, err := bootstrap.NewConfigFromContents([]byte(bootstrapConfig))
	if err != nil {
		return nil, err
	}

	coreConfig, err := buildXDSClientConfig(config, nil, "", time.Second)
	if err != nil {
		return nil, err
	}

	// Override the transport builder
	fb := NewFakeTransportBuilder()
	coreConfig.TransportBuilder = fb

	client, err := xdsclient.New(coreConfig)
	if err != nil {
		return nil, err
	}

	return &Fuzzer{
		xdsClient:        client,
		transportBuilder: fb,
		activeWatches:    make(map[string]func()),
	}, nil
}

func (fz *Fuzzer) Close() {
	if fz.xdsClient != nil {
		fz.xdsClient.Close()
	}
}

func (fz *Fuzzer) Act(action *Action) {
	if fz.xdsClient == nil {
		return
	}

	switch op := action.ActionType.(type) {
	case *Action_StartWatch:
		fz.handleStartWatch(op.StartWatch)
	case *Action_StopWatch:
		fz.handleStopWatch(op.StopWatch)
	case *Action_TriggerConnectionFailure:
		fz.handleConnectionFailure(op.TriggerConnectionFailure)
	case *Action_SendMessageToClient:
		fz.handleSendMessage(op.SendMessageToClient)
	case *Action_DumpCsdsData:
		fz.handleDumpCsdsData(op.DumpCsdsData)
	case *Action_ReadMessageFromClient:
		fz.handleReadMessage(op.ReadMessageFromClient)
	}
}

func (fz *Fuzzer) handleStartWatch(op *StartWatch) {
	if op == nil {
		return
	}

	if op.ResourceType == nil {
		return
	}

	var typeURL string
	switch op.ResourceType.ResourceType.(type) {
	case *ResourceType_Listener:
		typeURL = "type.googleapis.com/envoy.config.listener.v3.Listener"
	case *ResourceType_RouteConfig:
		typeURL = "type.googleapis.com/envoy.config.route.v3.RouteConfiguration"
	case *ResourceType_Cluster:
		typeURL = "type.googleapis.com/envoy.config.cluster.v3.Cluster"
	case *ResourceType_Endpoint:
		typeURL = "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment"
	default:
		return
	}

	resourceName := op.ResourceName
	cancel := fz.xdsClient.WatchResource(typeURL, resourceName, &fuzzWatcher{})
	fz.activeWatches[resourceName] = cancel
}

func (fz *Fuzzer) handleStopWatch(op *StopWatch) {
	if op == nil {
		return
	}
	if cancel, ok := fz.activeWatches[op.ResourceName]; ok {
		cancel()
		delete(fz.activeWatches, op.ResourceName)
	}
}

func (fz *Fuzzer) handleConnectionFailure(op *TriggerConnectionFailure) {
	if op == nil {
		return
	}

	transport := fz.transportBuilder.GetTransport(op.Authority)
	if transport != nil && transport.ActiveAdsStream != nil {
		transport.ActiveAdsStream.Close()
	}
}

func (fz *Fuzzer) handleSendMessage(op *SendMessageToClient) {
	if op == nil {
		return
	}
	// Send a raw response message to the client.
	// We need to find the stream.
	authority := ""
	if op.StreamId != nil {
		authority = op.StreamId.Authority
	}
	transport := fz.transportBuilder.GetTransport(authority)
	if transport != nil && transport.ActiveAdsStream != nil {
		transport.ActiveAdsStream.InjectRawResponse(op.RawResponseBytes)
	}
}

func (fz *Fuzzer) handleDumpCsdsData(op *DumpCsdsData) {
	fmt.Println("Hi")
	if op == nil {
		return
	}
	// Verify DumpResources doesn't crash
	_, _ = fz.xdsClient.DumpResources()
}

func (fz *Fuzzer) handleReadMessage(op *ReadMessageFromClient) {
	if op == nil {
		return
	}
	authority := ""
	if op.StreamId != nil {
		authority = op.StreamId.Authority
	}
	transport := fz.transportBuilder.GetTransport(authority)
	if transport != nil && transport.ActiveAdsStream != nil {
		// Just consume the request to clear queue/verify it exists
		_, _ = transport.ActiveAdsStream.ReadRequest(time.Millisecond)
	}
}

// Watcher implementation
type fuzzWatcher struct{}

func (w *fuzzWatcher) ResourceChanged(data xdsclient.ResourceData, done func()) {
	// No-op for now
	if done != nil {
		done()
	}
}

func (w *fuzzWatcher) ResourceError(err error, done func()) {
	// No-op
	if done != nil {
		done()
	}
}

func (w *fuzzWatcher) AmbientError(err error, done func()) {
	// No-op
	if done != nil {
		done()
	}
}

func FuzzXdsClient(f *testing.F) {
	// Add a valid seed input from C++ fuzzer

	// (kBasicListener)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{
								Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "server.example.com"}}},
				{ActionType: &Action_ReadMessageFromClient{
					ReadMessageFromClient: &ReadMessageFromClient{
						StreamId: &StreamId{
							Authority: "",
							Method: &StreamId_Ads{
								Ads: &StreamId_EmptyMessage{}}},
						Ok: true}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{Authority: "", Method: &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "1",
						"nonce": "A",
						"type_url": "type.googleapis.com/envoy.config.listener.v3.Listener",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.listener.v3.Listener",
							"name": "server.example.com",
							"api_listener": {
								"api_listener": {
									"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
									"http_filters": [{
										"name": "router",
										"typed_config": {"@type":"type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"}
									}],
									"rds": {
										"route_config_name": "route_config",
										"config_source": {"self": {}}
									}
								}
							}
						}]
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (kBasicCluster)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Cluster{
								Cluster: &ResourceType_EmptyMessage{}}},
						ResourceName: "cluster1"}}},
				{ActionType: &Action_ReadMessageFromClient{
					ReadMessageFromClient: &ReadMessageFromClient{
						StreamId: &StreamId{
							Authority: "",
							Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
						Ok: true}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{
						Authority: "",
						Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "1",
						"nonce": "A",
						"type_url": "type.googleapis.com/envoy.config.cluster.v3.Cluster",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.cluster.v3.Cluster",
							"name": "cluster1",
							"type": "EDS",
							"eds_cluster_config": {
								"eds_config": {"ads": {}},
								"service_name": "endpoint1"
							}
						}]
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (kBasicRoute)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_RouteConfig{
								RouteConfig: &ResourceType_EmptyMessage{}}},
						ResourceName: "route_config"}}},
				{ActionType: &Action_ReadMessageFromClient{
					ReadMessageFromClient: &ReadMessageFromClient{
						StreamId: &StreamId{
							Authority: "",
							Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
						Ok: true}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{
						Authority: "",
						Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "1",
						"nonce": "A",
						"type_url": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
							"name": "route_config",
							"virtual_hosts": [{
								"name": "vhost1",
								"domains": ["*"],
								"routes": [{"match": {"prefix": "/"}, "direct_response": {"status": 200}}]
							}]
						}]
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (kBasicEndpoint)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Endpoint{
								Endpoint: &ResourceType_EmptyMessage{}}},
						ResourceName: "endpoint1"}}},
				{ActionType: &Action_ReadMessageFromClient{
					ReadMessageFromClient: &ReadMessageFromClient{
						StreamId: &StreamId{
							Authority: "",
							Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
						Ok: true}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{
						Authority: "",
						Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "1",
						"nonce": "A",
						"type_url": "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.endpoint.v3.ClusterLoadAssignment",
							"cluster_name": "endpoint1",
							"endpoints": [{
								"lb_endpoints": [{
									"endpoint": {
										"address": {
											"socket_address": {
												"address": "127.0.0.1",
												"port_value": 8080
											}
										}
									}
								}]
							}]
						}]
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (XdsServersEmpty)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": []}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "\003"}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (ResourceWrapperEmpty)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Cluster{Cluster: &ResourceType_EmptyMessage{}}},
						ResourceName: "cluster"}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{Method: &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "envoy.config.cluster.v3.Cluster",
						"resources": [{"@type": "type.googleapis.com/envoy.service.discovery.v3.Resource"}],
						"canary": true,
						"type_url": "envoy.config.cluster.v3.Cluster",
						"nonce": "envoy.config.cluster.v3.Cluster"
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (RlsMissingTypedExtensionConfig)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_RouteConfig{RouteConfig: &ResourceType_EmptyMessage{}}},
						ResourceName: "route_config"}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{Method: &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "grpc.lookup.v1.RouteLookup",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
							"value": "CAFiAA=="
						}],
						"type_url": "envoy.config.route.v3.RouteConfiguration",
						"nonce": "/@\001\000\\\000\000x141183468234106731687303715884105729"
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (SendMessageToClientBeforeStreamCreated)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{Method: &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// (UnsubscribeWhileAdsCallInBackoff)
	f.Add(func() []byte {
		m := &Msg{
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "server.example.com"}}},
				// Trigger connection failure to induce backoff
				{ActionType: &Action_TriggerConnectionFailure{TriggerConnectionFailure: &TriggerConnectionFailure{}}},
				{ActionType: &Action_StopWatch{
					StopWatch: &StopWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "server.example.com"}}},
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{Method: &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
						"version_info": "1",
						"nonce": "A",
						"type_url": "type.googleapis.com/envoy.config.listener.v3.Listener",
						"resources": [{
							"@type": "type.googleapis.com/envoy.config.listener.v3.Listener",
							"name": "server.example.com",
							"api_listener": {
								"api_listener": {
									"@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
									"http_filters": [{"name": "router", "typed_config": {"@type":"type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"}}],
									"rds": {
										"route_config_name": "route_config",
										"config_source": {"self": {}}
									}
								}
							}
						}]
					}`),
				}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	// "Orphaned RDS" scenario.
	f.Add(func() []byte {
		m := &Msg{
			// 0. Bootstrap (Standard insecure xDS server)
			Bootstrap: `{"xds_servers": [{"server_uri":"xds.example.com", "channel_creds":[{"type": "insecure"}]}]}`,
			Actions: []*Action{
				// 1. Action_StartWatch(LDS, "ListenerA")
				{ActionType: &Action_StartWatch{
					StartWatch: &StartWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{
								Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "ListenerA"}}},

				// 2. Action_SendMessage(RDS, "RouteX") - ORPHANED INJECTION
				// This is the "premature" update.
				// Note: We send an RDS response, but the client hasn't asked for it yet.
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{
						Authority: "",
						Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
                    "version_info": "1",
                    "nonce": "A",
                    "type_url": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
                    "resources": [{
                        "@type": "type.googleapis.com/envoy.config.route.v3.RouteConfiguration",
                        "name": "RouteX",
                        "virtual_hosts": [{
                            "name": "vhost1",
                            "domains": ["*"],
                            "routes": [{"match": {"prefix": "/"}, "direct_response": {"status": 200}}]
                        }]
                    }]
                }`),
				}}},

				// 3. Action_SendMessage(LDS, "ListenerA" -> "RouteX")
				// Now the Listener arrives, pointing to the RouteX we just sent.
				{ActionType: &Action_SendMessageToClient{SendMessageToClient: &SendMessageToClient{
					StreamId: &StreamId{
						Authority: "",
						Method:    &StreamId_Ads{Ads: &StreamId_EmptyMessage{}}},
					RawResponseBytes: []byte(`{
                    "version_info": "1",
                    "nonce": "B",
                    "type_url": "type.googleapis.com/envoy.config.listener.v3.Listener",
                    "resources": [{
                        "@type": "type.googleapis.com/envoy.config.listener.v3.Listener",
                        "name": "ListenerA",
                        "api_listener": {
                            "api_listener": {
                                "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
                                "rds": {
                                    "route_config_name": "RouteX",
                                    "config_source": {"ads": {}}
                                },
                                "http_filters": [{"name": "router", "typed_config": {"@type":"type.googleapis.com/envoy.extensions.filters.http.router.v3.Router"}}]
                            }
                        }
                    }]
                }`),
				}}},

				// 4. Action_TriggerConnectionFailure
				// Stream breaks right after the LDS update.
				{ActionType: &Action_TriggerConnectionFailure{
					TriggerConnectionFailure: &TriggerConnectionFailure{
						Authority: "",
						Status:    &Status{Code: 14, Message: "Simulated Connection Failure"}, // 14 = UNAVAILABLE
					}}},

				// 5. Action_StopWatch(LDS, "ListenerA")
				// User gives up while we are reconnecting.
				{ActionType: &Action_StopWatch{
					StopWatch: &StopWatch{
						ResourceType: &ResourceType{
							ResourceType: &ResourceType_Listener{
								Listener: &ResourceType_EmptyMessage{}}},
						ResourceName: "ListenerA"}}},
			},
		}
		b, _ := proto.Marshal(m)
		return b
	}())

	f.Fuzz(func(t *testing.T, data []byte) {
		scenario := &Msg{}
		if err := proto.Unmarshal(data, scenario); err != nil {
			return
		}

		// Ensure that we don't crash the fuzzer due to panics in the system under test.
		// We expect the system to be robust, but for fuzzing stability, we catch panics.
		defer func() {
			if r := recover(); r != nil {
				// We must fail the test so the fuzzer knows this input caused a crash.
				// If we just log, the fuzzer considers it a pass.
				t.Fatalf("Recovered from panic: %v", r)
			}
		}()

		fz, err := NewFuzzer(t, scenario.Bootstrap)
		if err != nil {
			return
		}
		defer fz.Close()

		for _, action := range scenario.Actions {
			fz.Act(action)
		}
	})
}
