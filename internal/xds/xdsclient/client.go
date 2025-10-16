/*
 *
 * Copyright 2019 gRPC authors.
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

// Package xdsclient implements a full fledged gRPC client for the xDS API used
// by the xds resolver and balancer implementations.
package xdsclient

import (
	"context"

	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/internal/xds/clients/lrsclient"
	"google.golang.org/grpc/internal/xds/clients/xdsclient"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
)

// XDSClient is a full fledged gRPC client which queries a set of discovery APIs
// (collectively termed as xDS) on a remote management server, to discover
// various dynamic resources.
type XDSClient interface {
	// WatchResourceV2 matches the API of the external xdsclient interface.
	WatchResource(typeURL, resourceName string, watcher xdsclient.ResourceWatcher) (cancel func())

	ReportLoad(*bootstrap.ServerConfig) (*lrsclient.LoadStore, func(context.Context))

	BootstrapConfig() *bootstrap.Config
}

// DumpResources returns the status and contents of all xDS resources. It uses
// xDS clients from the default pool.
func DumpResources() *v3statuspb.ClientStatusResponse {
	return DefaultPool.DumpResources()
}
