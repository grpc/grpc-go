/*
 *
 * Copyright 2022 gRPC authors.
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

// Package credentials supports registering and retreiving credentials for xDS
// communications.
//
// Notice: This package is EXPERIMENTAL and may be changed or removed in a later
// release.
package credentials

import (
	"encoding/json"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/google"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// registry is a map from credential type name to Credential builder.
	registry = make(map[string]Builder)
)

// Builder interface encapsulates a credential provider
// that can be used for communicating with the xDS Management server.
type Builder interface {
	// BuildCredsBundle() returns a credential bundle associated with the Builder.
	BuildCredsBundle(config json.RawMessage) credentials.Bundle
	// Name() returns the credential name associated with this credential.
	Name() string
}

// GoogleDefaultCredsBuilder encapsulates a Google Default credential to satisfy the Builder interface.
type GoogleDefaultCredsBuilder struct{}

// BuildCredsBundle returns a default google credential bundle. Currently the JSON
// config is unused.
func (d *GoogleDefaultCredsBuilder) BuildCredsBundle(_ json.RawMessage) credentials.Bundle {
	return google.NewDefaultCredentials()
}

// Name returns the name associated with GoogleDefaultCredsBuilder i.e. "google_default".
func (d *GoogleDefaultCredsBuilder) Name() string {
	return "google_default"
}

// InsecureCredsBuilder encapsulates a Insecure credential to satisfy the Builder interface.
type InsecureCredsBuilder struct{}

// BuildCredsBundle returns a default insecure credential bundle. Currently the JSON
// config is unused.
func (i *InsecureCredsBuilder) BuildCredsBundle(_ json.RawMessage) credentials.Bundle {
	return insecure.NewBundle()
}

// Name returns the name associated with InsecureCredsBuilder i.e. "insecure".
func (i *InsecureCredsBuilder) Name() string {
	return "insecure"
}

func init() {
	Register(&GoogleDefaultCredsBuilder{})
	Register(&InsecureCredsBuilder{})
}

// Register registers the credential builder that can be used to communicate
// with the xds management server.
//
// NOTE: this function must only be called during initialization time (i.e. in
// an init() function), and is not thread-safe. If multiple credentials are
// registered with the same name, the one registered last will take effect.
func Register(b Builder) {
	registry[b.Name()] = b
}

// Get returns the credentials bundle associated with a given name.
// If no credentials are registered with the name, nil will be returned.
func Get(name string, config json.RawMessage) credentials.Bundle {
	if c, ok := registry[name]; ok {
		return c.BuildCredsBundle(config)
	}

	return nil
}
