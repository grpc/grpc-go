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

// GetGRPCServer returns the underlying gRPC server of an xds.GRPCServer. It
// returns nil if the xDS server is nil or its underlying server is not a
// *grpc.Server, as can happen with test implementations. It is initialized by
// package xds.
//
// The xDS server retains ownership of the underlying server. Callers must use
// the xDS server's Serve, Stop, and GracefulStop methods.
var GetGRPCServer any // func(*xds.GRPCServer) *grpc.Server
