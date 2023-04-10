/*
 *
 * Copyright 2023 gRPC authors.
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

package authz

import (
	"encoding/json"
	"sync"
)

// loggerBuilderRegistry holds a map of audit logger builders and a mutex
// to facilitate thread-safe reading/writing operations.
type loggerBuilderRegistry struct {
	mu       sync.Mutex
	builders map[string]AuditLoggerBuilder
}

var (
	registry = loggerBuilderRegistry{
		builders: make(map[string]AuditLoggerBuilder),
	}
)

// RegisterAuditLoggerBuilder registers the builder in a global map
// using b.Name() as the key.
// This should only be called during initialization time (i.e. in an init()
// function).
// If multiple builders are registered with the same name, the one registered
// last will take effect.
func RegisterAuditLoggerBuilder(b AuditLoggerBuilder) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.builders[b.Name()] = b
}

// GetAuditLoggerBuilder returns a builder with the given name.
// It returns nil if the builder is not found in the registry.
func GetAuditLoggerBuilder(name string) AuditLoggerBuilder {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.builders[name]
}

// AuditEvent contains information used by the audit logger during an audit
// logging event.
type AuditEvent struct {
	// FullMethodName is the full method name of the audited RPC, in the format
	// of "/pkg.Service/Method". For example, "/helloworld.Greeter/SayHello".
	FullMethodName string
	// Principal is the identity of the caller. Currently it will only be
	// available in certificate-based TLS authentication.
	Principal string
	// PolicyName is the authorization policy name or the xDS RBAC filter name.
	PolicyName string
	// MatchedRule is the matched rule or policy name in the xDS RBAC filter.
	// It will be empty if there is no match.
	MatchedRule string
	// Authorized indicates whether the audited RPC is authorized or not.
	Authorized bool
}

// AuditLoggerConfig defines the configuration for a particular implementation
// of audit logger.
type AuditLoggerConfig interface {
	// auditLoggerConfig is a dummy interface requiring users to embed this
	// interface to implement it.
	auditLoggerConfig()
}

// AuditLogger is the interface for an audit logger.
// An audit logger is a logger instance that can be configured to use via the
// authorization policy or xDS HTTP RBAC filters. When the authorization
// decision meets the condition for audit, all the configured audit loggers'
// Log() method will be invoked to log that event with the AuditInfo.
// The method will be executed synchronously before the authorization is
// complete and the call is denied or allowed.
//
// TODO(lwge): Change the link to the merged gRFC once it's ready.
// Please refer to https://github.com/grpc/proposal/pull/346 for more details
// about audit logging.
type AuditLogger interface {
	// Log does audit logging with the given information in the audit event.
	// This method will be executed synchronously by gRPC so implementers must
	// keep in mind it must not block the RPC. Specifically, time-consuming
	// processes should be fired asynchronously such that this method can
	// return immediately.
	Log(*AuditEvent)
}

// AuditLoggerBuilder is the interface for an audit logger builder.
// It parses and validates a config, and builds an audit logger from the parsed
// config. This enables configuring and instantiating audit loggers in the
// runtime. Users that want to implement their own audit logging logic should
// implement this along with the AuditLogger interface and register this
// builder by calling RegisterAuditLoggerBuilder() before they start the gRPC
// server.
//
// TODO(lwge): Change the link to the merged gRFC once it's ready.
// Please refer to https://github.com/grpc/proposal/pull/346 for more details
// about audit logging.
type AuditLoggerBuilder interface {
	// ParseAuditLoggerConfig parses the given JSON bytes into a structured
	// logger config this builder can use to build an audit logger.
	// When users implement this method, its return type must embed the
	// AuditLoggerConfig interface.
	ParseAuditLoggerConfig(config json.RawMessage) (AuditLoggerConfig, error)
	// Build builds an audit logger with the given logger config.
	// This will only be called with valid configs returned from
	// ParseAuditLoggerConfig() and any runtime issues such as failing to
	// create a file should be handled by the logger implementation instead of
	// failing the logger instantiation. So implementers need to make sure it
	// can return a logger without error at this stage.
	Build(AuditLoggerConfig) AuditLogger
	// Name returns the name of logger built by this builder.
	// This is used to register and pick the builder.
	Name() string
}
