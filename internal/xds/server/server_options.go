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
)

type resourceNameOption struct {
	grpc.EmptyServerOption
	f func(net.Addr) string
}

// ResourceNameFunc returns a server option that overrides the LDS resource
// name selected for an xDS server listener.
func ResourceNameFunc(f func(net.Addr) string) grpc.ServerOption {
	return &resourceNameOption{f: f}
}

// ResourceNameFuncFromServerOption returns the resource name function carried
// by opt, if opt was created by ResourceNameFunc.
func ResourceNameFuncFromServerOption(opt grpc.ServerOption) (func(net.Addr) string, bool) {
	o, ok := opt.(*resourceNameOption)
	if !ok {
		return nil, false
	}
	return o.f, true
}
