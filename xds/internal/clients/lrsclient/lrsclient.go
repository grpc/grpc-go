//revive:disable:unused-parameter

/*
 *
 * Copyright 2025 gRPC authors.
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

// Package lrsclient provides an LRS (Load Reporting Service) client.
//
// See: https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/load_stats/v3/lrs.proto
package lrsclient

import (
	"google.golang.org/grpc/xds/internal/clients"
)

// LRSClient is an LRS (Load Reporting Service) client.
type LRSClient struct {
}

// New returns a new LRS Client configured with the provided config.
func New(config Config) (*LRSClient, error) {
	panic("unimplemented")
}

// ReportLoad creates a new load reporting stream for the client. It creates a
// LoadStore and return it for the caller to report loads.
func (c *LRSClient) ReportLoad(serverConfig clients.ServerConfig) *LoadStore {
	panic("unimplemented")
}

// Close closes the LRS client.
func (c *LRSClient) Close() error {
	panic("unimplemented")
}
