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

// Package unix implements a resolver for unix targets.
package unix

import (
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

const scheme = "unix"

type unixBuilder struct{}

func (*unixBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	cc.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: target.Endpoint, Attributes: attributes.New("network_type", "unix")}}})
	return &unixResolver{}, nil
}

func (*unixBuilder) Scheme() string {
	return scheme
}

type unixResolver struct {
}

func (*unixResolver) ResolveNow(o resolver.ResolveNowOptions) {}

func (*unixResolver) Close() {}

func init() {
	resolver.Register(&unixBuilder{})
}
