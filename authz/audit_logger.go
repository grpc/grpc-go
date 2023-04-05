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
	"context"
	"sync"
)

var (
	mu sync.Mutex
	// m is a map from the name to the audit logger builder.
	// This backs the RegisterAuditLoggerBuilder API.
	m = make(map[string]AuditLoggerBuilder)
)

// RegisterAuditLoggerBuilder registers the builder in a global map
// using b.Name() as the key.
// This should only be called during initialization time (i.e. in an init() function).
// If multiple builders are registered with the same name, the one registered last
// will take effect.
func RegisterAuditLoggerBuilder(b AuditLoggerBuilder) {
	mu.Lock()
	defer mu.Unlock()
	m[b.Name()] = b
}

// AuditInfo contains information used by the audit logger during an audit logging event.
type AuditInfo struct {
	// RPCMethod is the method of the audited RPC, in the format of "/pkg.Service/Method".
	// For example, "/helloworld.Greeter/SayHello".
	RPCMethod string `json:"rpc_method,omitempty"`
	// Principal is the identity of the RPC. Currently it will only be available in
	// certificate-based TLS authentication.
	Principal string `json:"principal,omitempty"`
	// PolicyName is the authorization policy name (or the xDS RBAC filter name).
	PolicyName string `json:"policy_name,omitempty"`
	// MatchedRule is the matched rule (or policy name in the xDS RBAC filter). It will be
	// empty if there is no match.
	MatchedRule string `json:"matched_rule,omitempty"`
	// Authorized indicates whether the audited RPC is authorized or not.
	Authorized bool `json:"bool,omitempty"`
}

// AuditLoggerConfig defines the configuration for a particular implementation of audit logger.
type AuditLoggerConfig interface {
	// Name returns the same name as that returned by its supported builder.
	Name() string
	// auditLoggerConfig is a dummy interface requiring users to embed this
	// interface to implement it.
	auditLoggerConfig()
}

// AuditLogger is the interface for an audit logger.
// An audit logger is a logger instance that can be configured to use via the authorization policy
// or xDS HTTP RBAC filters. When the authorization decision meets the condition for audit, all the
// configured audit loggers' Log() method will be invoked to log that event with the AuditInfo.
// The method will be executed synchronously before the authorization is complete and the call is
// denied or allowed.
// Please refer to https://github.com/grpc/proposal/pull/346 for more details about audit logging.
type AuditLogger interface {
	// Log logs the auditing event with the given information.
	Log(context.Context, *AuditInfo)
}

// AuditLoggerBuilder is the interface for an audit logger builder.
// It parses and validates a config, and builds an audit logger from the parsed config. This enables
// configuring and instantiating audit loggers in the runtime.
// Users that want to implement their own audit logging logic should implement this along with
// the AuditLogger interface and register this builder by calling RegisterAuditLoggerBuilder()
// before they start the gRPC server.
// Please refer to https://github.com/grpc/proposal/pull/346 for more details about audit logging.
type AuditLoggerBuilder interface {
	// ParseAuditLoggerConfig parses an implementation-specific config into a
	// structured logger config this builder can use to build an audit logger.
	// When users implement this method, its returned type must embed the
	// AuditLoggerConfig interface.
	ParseAuditLoggerConfig(config interface{}) (AuditLoggerConfig, error)
	// Build builds an audit logger with the given logger config.
	// This will only be called with valid configs returned from ParseAuditLoggerConfig()
	// so implementers need to make sure it can return a logger without error
	// at this stage.
	Build(AuditLoggerConfig) AuditLogger
	// Name returns the name of logger built by this builder.
	// This is used to register and pick the builder.
	Name() string
}

// GetAuditLoggerBuilder returns a builder with the given name.
// It returns nil if the builder is not found in the registry.
func GetAuditLoggerBuilder(name string) AuditLoggerBuilder {
	mu.Lock()
	defer mu.Unlock()
	return m[name]
}
