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

package lrsclient

import (
	"google.golang.org/grpc/xds/clients"
	"google.golang.org/grpc/xds/clients/lrsclient/load"
)

// LRSClient represents an LRS client.
type LRSClient struct {
	config *Config
}

// ReportLoad starts a load reporting stream to the given server. All load
// reports to the same server share the LRS stream.
//
// It returns a `*load.Store` for the user to report loads, a function to cancel
// the load reporting stream.
//
// The first call to `ReportLoad` sets the reference count to one, and starts the
// LRS streaming call. Subsequent calls increment the reference count and return
// the same load.Store.
//
// The cleanup function decrements the reference count and stops the LRS stream
// when the last reference is removed.
//
// The stats from `*load.Store` are reported periodically until cleanup function is // called
func (c *LRSClient) ReportLoad(server *clients.ServerConfig) (*load.Store, func()) {
	return load.New(), func() {}
}
