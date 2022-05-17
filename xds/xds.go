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
	"fmt"

	v3statusgrpc "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/internal"
	internaladmin "google.golang.org/grpc/internal/admin"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/xds/csds"
	"google.golang.org/grpc/xds/internal/clusterspecifier/rls"
	"google.golang.org/grpc/xds/internal/httpfilter/rbac"

	_ "google.golang.org/grpc/credentials/tls/certprovider/pemfile"         // Register the file watcher certificate provider plugin.
	_ "google.golang.org/grpc/xds/internal/balancer"                        // Register the balancers.
	_ "google.golang.org/grpc/xds/internal/clusterspecifier/rls"            // Register the RLS cluster specifier plugin. Note that this does not register the RLS LB policy.
	_ "google.golang.org/grpc/xds/internal/httpfilter/fault"                // Register the fault injection filter.
	_ "google.golang.org/grpc/xds/internal/httpfilter/rbac"                 // Register the RBAC filter.
	_ "google.golang.org/grpc/xds/internal/httpfilter/router"               // Register the router filter.
	xdsresolver "google.golang.org/grpc/xds/internal/resolver"              // Register the xds_resolver.
	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v2" // Register the v2 xDS API client.
	_ "google.golang.org/grpc/xds/internal/xdsclient/controller/version/v3" // Register the v3 xDS API client.
)

func init() {
	internaladmin.AddService(func(registrar grpc.ServiceRegistrar) (func(), error) {
		var grpcServer *grpc.Server
		switch ss := registrar.(type) {
		case *grpc.Server:
			grpcServer = ss
		case *GRPCServer:
			sss, ok := ss.gs.(*grpc.Server)
			if !ok {
				logger.Warningf("grpc server within xds.GRPCServer is not *grpc.Server, CSDS will not be registered")
				return nil, nil
			}
			grpcServer = sss
		default:
			// Returning an error would cause the top level admin.Register() to
			// fail. Log a warning instead.
			logger.Warningf("server to register service on is neither a *grpc.Server or a *xds.GRPCServer, CSDS will not be registered")
			return nil, nil
		}

		csdss, err := csds.NewClientStatusDiscoveryServer()
		if err != nil {
			return nil, fmt.Errorf("failed to create csds server: %v", err)
		}
		v3statusgrpc.RegisterClientStatusDiscoveryServiceServer(grpcServer, csdss)
		return csdss.Close, nil
	})

	internal.NewXDSResolverWithConfigForTesting = func(bootstrapConfig []byte) (resolver.Builder, error) {
		return xdsresolver.NewBuilderForTesting(bootstrapConfig)
	}
}

// RegisterRLSClusterSpecifierPluginForTesting registers the RLS Cluster
// Specifier Plugin for testing purposes, regardless of the XDSRLS environment
// variable.
//
// TODO: Remove this function once the RLS env var is removed.
//
// Testing Only
//
// This function should ONLY be used for testing purposes.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and will be removed in a later release.
func RegisterRLSClusterSpecifierPluginForTesting() {
	rls.RegisterForTesting()
}

// UnregisterRLSClusterSpecifierPluginForTesting unregisters the RLS Cluster
// Specifier Plugin for testing purposes. This is needed because there is no way
// to unregister the RLS Cluster Specifier Plugin after registering it solely
// for testing purposes using RegisterRLSClusterSpecifierPluginForTesting().
//
// TODO: Remove this function once the RLS env var is removed.
//
// Testing Only
//
// This function should ONLY be used for testing purposes.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and will be removed in a later release.
func UnregisterRLSClusterSpecifierPluginForTesting() {
	rls.UnregisterForTesting()
}

// RegisterRBACHTTPFilterForTesting registers the RBAC HTTP Filter for testing
// purposes, regardless of the RBAC environment variable.
//
// TODO: Remove this function once the RBAC env var is removed.
//
// Testing Only
//
// This function should ONLY be used for testing purposes.
//
// Experimental
//
// Notice: This API is EXPERIMENTAL and will be removed in a later release.
func RegisterRBACHTTPFilterForTesting() {
	rbac.RegisterForTesting()
}

// UnregisterRBACHTTPFilterForTesting unregisters the RBAC HTTP Filter for
// testing purposes. This is needed because there is no way to unregister the
// HTTP Filter after registering it solely for testing purposes using
// RegisterRBACHTTPFilterForTesting().
//
// TODO: Remove this function once the RBAC env var is removed.
func UnregisterRBACHTTPFilterForTesting() {
	rbac.UnregisterForTesting()
}
