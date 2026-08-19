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
 */

package server

import (
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/internal/xds/xdsclient"
)

// ServerOptions contains options used by an xDS-enabled gRPC server.
//
// This type is internal so that the public xds package can expose server
// options without also owning their application and storage.
type ServerOptions struct {
	ModeCallback                 ServingModeCallback
	ClientPoolForTesting         *xdsclient.Pool
	OverrideListenerResourceName func(net.Addr) string
}

type serverOption struct {
	grpc.EmptyServerOption
	apply func(*ServerOptions)
}

// NewServerOption returns a grpc.ServerOption which applies f to the internal
// xDS server options.
func NewServerOption(f func(*ServerOptions)) grpc.ServerOption {
	return &serverOption{apply: f}
}

// ApplyServerOptions applies all internal xDS server options in opts to so.
func ApplyServerOptions(opts []grpc.ServerOption, so *ServerOptions) {
	for _, opt := range opts {
		if o, ok := opt.(*serverOption); ok {
			o.apply(so)
		}
	}
}

// OverrideListenerResourceName returns a server option that overrides the LDS
// resource name selected for an xDS server listener.
func OverrideListenerResourceName(f func(net.Addr) string) grpc.ServerOption {
	return NewServerOption(func(o *ServerOptions) {
		o.OverrideListenerResourceName = f
	})
}
