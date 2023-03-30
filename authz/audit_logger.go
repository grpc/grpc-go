package authz

import (
	"context"
	"sync"
)

var (
	mu sync.Mutex
	m  = make(map[string]AuditLoggerBuilder)
)

// ReigsterAuditLoggerBuilder registers the builder in a global map
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
	RPCMethod   string `json:"rpc_method,omitempty"`
	Principal   string `json:"principal,omitempty"`
	PolicyName  string `json:"policy_name,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
	Authorized  bool   `json:"bool,omitempty"`
}

// AuditLoggerConfig defines the configuration for a particular implementation of audit logger.
// The returned value from Name() should serve as a sanity check against the Name() of the
// logger builder.
type AuditLoggerConfig interface {
	Name() string
}

// AuditLogger is the interface for an audit logger.
type AuditLogger interface {
	Log(context.Context, *AuditInfo)
}

// AuditLoggerBuilder is the interface for an audit logger builder.
type AuditLoggerBuilder interface {
	ParseAuditLoggerConfig(config interface{}) (AuditLoggerConfig, error)
	Build(AuditLoggerConfig) AuditLogger
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
