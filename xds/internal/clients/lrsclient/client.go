/*
 *
 * Copyright 2024 gRPC authors.
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

// Package lrsclient provides implementation of the LRS client for enabling
// applications to report load to the xDS management servers.
//
// It allows applications to report load data to an LRS server via the LRS
// stream. This data can be used for monitoring, traffic management, and other
// purposes.
package lrsclient

import (
	"google.golang.org/grpc/xds/internal/clients"
)

// LRSClient is a full fledged LRS client.
type LRSClient struct {
}

// ReportLoad starts a load reporting stream to the given server. All load
// reports to the same server share the LRS stream.
//
// It returns a [LoadStore] for the user to report loads, a function to
// cancel the load reporting stream.
//
// The stats from [LoadStore] are reported periodically until cleanup
// function is called.
func (c *LRSClient) ReportLoad(_ *clients.ServerConfig) (*LoadStore, func()) {
	return NewLoadStore(), func() {}
}
