package audit

import (
	"encoding/json"
	"fmt"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
)

//TODO we should have somewhere logic related to
//1. checking if logger type is known
//2. if yes, check if it's config is valid (LoggerBuilder.ParseLoggerConfig)
//IMHO it belongs to json parsing layer but not here

func ConvertXdsAuditLoggerConfig(loggerCfg *v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig) (json.RawMessage, error) {
	if loggerCfg == nil {
		return nil, fmt.Errorf("rbac audit logging: nil AuditLoggerConfig message provided")
	}
	if loggerCfg.AuditLogger == nil {
		return nil, fmt.Errorf("rbac audit logging: AuditLogger type is mandatory")
	}
	jsonBytes, err := json.Marshal(loggerCfg)
	if err != nil {
		return nil, fmt.Errorf("rbac audit logging: failed to marshal AuditLoggerConfig to json: %v", err)
	}
	return jsonBytes, nil
}

func ExtractXdsAuditLoggersConfig(optionsCfg *v3rbacpb.RBAC_AuditLoggingOptions) ([]json.RawMessage, error) {
	if optionsCfg == nil {
		fmt.Println("rbac audit logging: nil AuditLoggingOptions message provided, audit is disabled")
		return nil, nil
	}
	if optionsCfg.LoggerConfigs == nil || len(optionsCfg.LoggerConfigs) == 0 {
		fmt.Println("rbac audit logging: no AuditLoggerConfigs found, audit is disabled")
		return nil, nil
	}
	validConfigs := make([]json.RawMessage, 0)
	for _, v := range optionsCfg.LoggerConfigs {
		jsonBytes, err := ConvertXdsAuditLoggerConfig(v)
		if err != nil {
			continue
		}
		validConfigs = append(validConfigs, jsonBytes)
	}

	return validConfigs, nil
}
