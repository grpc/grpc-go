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

package xds

import "net"

// Experimental
//
// Notice: All APIs in this file are EXPERIMENTAL and may be changed or removed
// in a later release.

// A ServerOption sets options which are specific to xDS functionality provided
// by GRPCServer. Options related to functionality provided by grpc.Server must
// be specified using grpc.ServerOption.
type ServerOption interface {
	apply(*serverOptions)
}

type serverOptions struct {
	modeCallback ServingModeCallback
}

// ServingModeServerOption is a ServerOption which allows users to register a
// callback to get notified about serving mode changes.
type ServingModeServerOption struct {
	Callback ServingModeCallback
}

func (s *ServingModeServerOption) apply(opts *serverOptions) {
	opts.modeCallback = s.Callback
}

// ServingMode indicates the current mode of operation of the server.
type ServingMode int

const (
	// ServingModeStarting indicates that the serving is starting up.
	ServingModeStarting ServingMode = iota
	// ServingModeServing indicates the the server contains all required xDS
	// configuration is serving RPCs.
	ServingModeServing
	// ServingModeNotServing indicates that the server is not accepting new
	// connections. Existing connections will be closed gracefully, allowing
	// in-progress RPCs to complete. A server enters this mode when it does not
	// contain the required xDS configuration to serve RPCs.
	ServingModeNotServing
)

func (s ServingMode) String() string {
	switch s {
	case ServingModeNotServing:
		return "not-serving"
	case ServingModeServing:
		return "serving"
	default:
		return "starting"
	}
}

// ServingModeCallback is the callback that users can register to get notified
// about the server's serving mode changes. The callback is invoked with the
// address of the listener and its new mode. The err parameter is set to a
// non-nil error if the server has transitioned into not-serving mode.
//
// Users must not perform any blocking operations in this callback.
type ServingModeCallback func(addr net.Addr, mode ServingMode, err error)
