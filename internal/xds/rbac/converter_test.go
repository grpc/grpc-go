package rbac

import (
	"strings"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/internal"
	"google.golang.org/protobuf/types/known/anypb"
)

func (s) TestBuildLoggerErrors(t *testing.T) {
	tests := []struct {
		name           string
		loggerConfig   *v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig
		expectedLogger audit.Logger
		expectedError  string
	}{
		{
			name: "nil typed config",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					TypedConfig: nil,
				},
			},
			expectedError: "AuditLogger TypedConfig cannot be nil",
		},
		{
			name: "Unsupported Type",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					Name:        "TestAuditLoggerBuffer",
					TypedConfig: &anypb.Any{},
				},
			},
			expectedError: "custom config not implemented for type ",
		},
		{
			name: "Empty name",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					Name:        "TestAuditLoggerBuffer",
					TypedConfig: createUDPATypedStruct(t, map[string]interface{}{}, ""),
				},
			},
			expectedError: "AuditLogger TypedConfig.TypeURL cannot be an empty string",
		},
		{
			name: "No registered logger",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					Name:        "UnregisteredLogger",
					TypedConfig: createUDPATypedStruct(t, map[string]interface{}{}, "UnregisteredLogger"),
				},
				IsOptional: false,
			},
			expectedError: "no builder registered for UnregisteredLogger",
		},
		{
			name: "fail to parse custom config",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					Name:        "TestAuditLoggerCustomConfig",
					TypedConfig: createUDPATypedStruct(t, map[string]interface{}{"abc": "BADVALUE", "xyz": "123"}, "TestAuditLoggerCustomConfig")},
				IsOptional: false,
			},
			expectedError: "audit logger custom config could not be parsed",
		},
		{
			name: "No registered logger but optional passes",
			loggerConfig: &v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
				AuditLogger: &v3corepb.TypedExtensionConfig{
					Name:        "UnregisteredLogger",
					TypedConfig: createUDPATypedStruct(t, map[string]interface{}{}, "UnregisteredLogger"),
				},
				IsOptional: true,
			},
			expectedLogger: nil,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := TestAuditLoggerCustomConfigBuilder{}
			audit.RegisterLoggerBuilder(&b)
			defer internal.UnregisterAuditLoggerBuilderForTesting(b.Name())
			logger, err := buildLogger(test.loggerConfig)
			if err != nil {
				if !strings.HasPrefix(err.Error(), test.expectedError) {
					t.Fatalf("expected error: %v. got error: %v", test.expectedError, err)
				}
			} else {
				if logger != test.expectedLogger {
					t.Fatalf("expected logger: %v. got logger: %v", test.expectedLogger, logger)
				}
			}

		})
	}

}
