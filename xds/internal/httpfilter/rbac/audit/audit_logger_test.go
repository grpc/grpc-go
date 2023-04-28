package audit

import (
	"strings"
	"testing"

	v3corepb "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestExtractXdsAuditLoggersConfig(t *testing.T) {
	tests := map[string]struct {
		auditLoggingOptions *v3rbacpb.RBAC_AuditLoggingOptions
		wantErr             string
		wantJsonCfg         string
	}{
		"valid std_out cfg": {
			auditLoggingOptions: &v3rbacpb.RBAC_AuditLoggingOptions{
				AuditCondition: v3rbacpb.RBAC_AuditLoggingOptions_NONE,
				LoggerConfigs: []*v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
					{AuditLogger: &v3corepb.TypedExtensionConfig{
						Name: "stdout_logger", TypedConfig: anyPbHelper(t, map[string]interface{}{})},
						IsOptional: true,
					},
				},
			},
			wantJsonCfg: `[{"audit_logger":{"name":"stdout_logger","typed_config":{"type_url":"type.googleapis.com/google.protobuf.Struct"}},"is_optional":true}]`,
		},
		"valid multi logger cfg": {
			auditLoggingOptions: &v3rbacpb.RBAC_AuditLoggingOptions{
				AuditCondition: v3rbacpb.RBAC_AuditLoggingOptions_NONE,
				LoggerConfigs: []*v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig{
					{AuditLogger: &v3corepb.TypedExtensionConfig{
						Name: "stdout_logger", TypedConfig: anyPbHelper(t, map[string]interface{}{})},
						IsOptional: true,
					},
					{AuditLogger: &v3corepb.TypedExtensionConfig{
						Name: "clients_magic_logger", TypedConfig: anyPbHelper(t, map[string]interface{}{})},
						IsOptional: false,
					},
				},
			},
			wantJsonCfg: `[{"audit_logger":{"name":"stdout_logger","typed_config":{"type_url":"type.googleapis.com/google.protobuf.Struct"}},"is_optional":true},
										 {"audit_logger":{"name":"clients_magic_logger","typed_config":{"type_url":"type.googleapis.com/google.protobuf.Struct"}}}]`,
		},
		"valid empty cfg": {
			auditLoggingOptions: &v3rbacpb.RBAC_AuditLoggingOptions{},
			wantJsonCfg:         "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			gotJsonCfg, gotErr := ExtractXdsAuditLoggersConfig(test.auditLoggingOptions)
			if gotErr != nil && !strings.HasPrefix(gotErr.Error(), test.wantErr) {
				t.Fatalf("unexpected error\nwant:%v\ngot:%v", test.wantErr, gotErr)
			}
			if diff := cmp.Diff(gotJsonCfg, test.wantJsonCfg, protocmp.Transform()); diff != "" {
				t.Fatalf("unexpected jsonconfig\ndiff (-want +got):\n%s", diff)
			}
		})
	}
}

func anyPbHelper(t *testing.T, in map[string]interface{}) *anypb.Any {
	t.Helper()
	pb, err := structpb.NewStruct(in)
	if err != nil {
		t.Fatal(err)
	}
	ret, err := anypb.New(pb)
	if err != nil {
		t.Fatal(err)
	}
	return ret
}
