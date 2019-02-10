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

package naming

import (
	"context"
	"net"
	"time"
)

// LookupSRVFn type for functions used to lookup SRV records.
type LookupSRVFn func(ctx context.Context, service, proto, name string) (cname string, addrs []*net.SRV, err error)

// LookupHostFn type for functions used to lookup A records.
type LookupHostFn func(ctx context.Context, host string) (addrs []string, err error)

type options struct {
	lookupSRV  LookupSRVFn
	lookupHost LookupHostFn

	// frequency of polling the DNS server that the watchers created by this resolver will use.
	freq time.Duration
}

var defaultOptions = options{
	freq:       defaultFreq,
	lookupSRV:  net.DefaultResolver.LookupSRV,
	lookupHost: net.DefaultResolver.LookupHost,
}

// A Option sets options such as frequency etc.
type Option func(*options)

// LookupSRV configures the resolver to create watchers that poll the DNS
// server using the specified LookupSRVFn.
func LookupSRV(f LookupSRVFn) Option {
	return func(o *options) {
		o.lookupSRV = f
	}
}

// LookupHost configures the resolver to create watchers that poll the DNS
// server using the specified LookupHostFn.
func LookupHost(f LookupHostFn) Option {
	return func(o *options) {
		o.lookupHost = f
	}
}

// Freq configures the resolver to create watchers that poll the DNS server
// with the specified frequency.
func Freq(freq time.Duration) Option {
	return func(o *options) {
		o.freq = freq
	}
}
