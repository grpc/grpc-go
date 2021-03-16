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

// Package admin contains internal implementation for admin service.
package admin

import "google.golang.org/grpc"

var (
	// services is a map from name to service register functions.
	services = make(map[string]func(grpc.ServiceRegistrar) (func(), error))
)

// AddService adds a service to the list of admin services.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple services are added
// with the same name, the one added last will take effect.
func AddService(name string, f func(grpc.ServiceRegistrar) (func(), error)) {
	services[name] = f
}

// Register registers the set of admin services to the given server.
func Register(s grpc.ServiceRegistrar) (cleanup func(), _ error) {
	var cleanups []func()
	for _, f := range services {
		cleanup, err := f(s)
		if err != nil {
			callFuncs(cleanups)
			return nil, err
		}
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
	}
	return func() {
		callFuncs(cleanups)
	}, nil
}

func callFuncs(fs []func()) {
	for _, f := range fs {
		f()
	}
}
