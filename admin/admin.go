/*
 *
 * Copyright 2021 gRPC authors.
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

// Package admin implements the admin service. It is a convenient API to expose
// states in the gRPC library.
package admin

import (
	"google.golang.org/grpc"
	channelzservice "google.golang.org/grpc/channelz/service"
	internaladmin "google.golang.org/grpc/internal/admin"
)

func init() {
	// Add a list of default services to admin here. Optional services, like
	// CSDS, will be added by other packages.
	internaladmin.AddService("channelz", func(registrar grpc.ServiceRegistrar) {
		channelzservice.RegisterChannelzServiceToServer(registrar)
	})
}

// Register registers the set of admin services to the given server.
//
// Note that this only supports `*grpc.Server` instead of
// `grpc.ServiceRegistrar`, because CSDS generated code isn't updated to support
// `grpc.ServiceRegistrar`.
func Register(s *grpc.Server) {
	// TODO: update this to `grpc.ServiceRegistrar` when CSDS generated code is
	// updated.
	//
	// https://github.com/envoyproxy/go-control-plane/issues/403
	internaladmin.Register(s)
}
