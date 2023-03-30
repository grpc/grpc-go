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

package authz

import (
	"sync"
)

var (
	mu sync.Mutex
	m  = make(map[string]AuditLoggerBuilder)
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
	// RPCMethod is the method of the audited RPC.
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
	// Name() returns the same name as that returned by its supported builder.
	Name() string
}

// AuditLogger is the interface for an audit logger.
type AuditLogger interface {
	// Log logs the auditing event with the given information.
	Log(*AuditInfo)
}

// AuditLoggerBuilder is the interface for an audit logger builder.
type AuditLoggerBuilder interface {
	// ParseAuditLoggerConfig parses an implementation-specific config into a
	// structured logger config this builder can use to build an audit logger.
	ParseAuditLoggerConfig(config interface{}) (AuditLoggerConfig, error)
	// Build builds an audit logger with the given logger config.
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
	if b, ok := m[name]; ok {
		return b
	}
	return nil
}
