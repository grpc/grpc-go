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

// Package xds contains an implementation of the xDS suite of protocols, to be
// used by gRPC client and server applications.
//
// On the client-side, users simply need to import this package to get all xDS
// functionality. On the server-side, users need to use the GRPCServer type
// exported by this package instead of the regular grpc.Server.
//
// See https://github.com/grpc/grpc-go/tree/master/examples/features/xds for
// example.
package xds

import (
	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/grpc"
	internaladmin "google.golang.org/grpc/internal/admin"
	"google.golang.org/grpc/xds/csds"

	_ "google.golang.org/grpc/credentials/tls/certprovider/pemfile" // Register the file watcher certificate provider plugin.
	_ "google.golang.org/grpc/xds/internal/balancer"                // Register the balancers.
	_ "google.golang.org/grpc/xds/internal/client/v2"               // Register the v2 xDS API client.
	_ "google.golang.org/grpc/xds/internal/client/v3"               // Register the v3 xDS API client.
	_ "google.golang.org/grpc/xds/internal/httpfilter/fault"        // Register the fault injection filter.
	_ "google.golang.org/grpc/xds/internal/resolver"                // Register the xds_resolver.
)

func init() {
	internaladmin.AddService("csds", func(registrar grpc.ServiceRegistrar) {
		s, ok := registrar.(*grpc.Server)
		if !ok {
			// This check is necessary because CSDS proto's register function
			// only accept *grpc.Server (it's using an old codegen):
			// https://github.com/envoyproxy/go-control-plane/blob/d456d987392e1325f0e06e170d2488b4f57e9623/envoy/service/status/v3/csds.pb.go#L824
			//
			// TODO: update CSDS's generated proto, and remove this check.
			// https://github.com/envoyproxy/go-control-plane/issues/403
			logger.Warningf("Server to register service on is not a *grpc.Server, CSDS will not be registered")
			return
		}
		v3statuspb.RegisterClientStatusDiscoveryServiceServer(s, csds.NewClientStatusDiscoveryServer())
	})
}
