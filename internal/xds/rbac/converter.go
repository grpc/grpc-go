/*
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
 */

package rbac

import (
	"encoding/json"
	"fmt"
	"strings"

	v1xdsudpatypepb "github.com/cncf/xds/go/udpa/type/v1"
	v3xdsxdstypepb "github.com/cncf/xds/go/xds/type/v3"
	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"google.golang.org/grpc/authz/audit"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
)

func buildLogger(loggerConfig *v3rbacpb.RBAC_AuditLoggingOptions_AuditLoggerConfig) (audit.Logger, error) {
	if loggerConfig.AuditLogger.TypedConfig == nil {
		return nil, fmt.Errorf("AuditLogger TypedConfig cannot be nil")
	}
	customConfig, loggerName, err := getCustomConfig(loggerConfig.AuditLogger.TypedConfig)
	if err != nil {
		return nil, err
	}
	if loggerName == "" {
		return nil, fmt.Errorf("AuditLogger TypedConfig.TypeURL cannot be an empty string")
	}
	factory := audit.GetLoggerBuilder(loggerName)
	if factory == nil {
		if loggerConfig.IsOptional {
			return nil, nil
		}
		return nil, fmt.Errorf("no builder registered for %v", loggerConfig.AuditLogger.TypedConfig.TypeUrl)
	}
	auditLoggerConfig, err := factory.ParseLoggerConfig(customConfig)
	if err != nil {
		return nil, fmt.Errorf("audit logger custom config could not be parsed: %v", err)
	}
	auditLogger := factory.Build(auditLoggerConfig)
	return auditLogger, nil
}

func getCustomConfig(config *anypb.Any) (json.RawMessage, string, error) {
	switch config.GetTypeUrl() {
	case "type.googleapis.com/udpa.type.v1.TypedStruct":
		typedStruct := &v1xdsudpatypepb.TypedStruct{}
		if err := config.UnmarshalTo(typedStruct); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal resource: %v", err)
		}
		return convertCustomConfig(typedStruct.TypeUrl, typedStruct.Value)
	case "type.googleapis.com/xds.type.v3.TypedStruct":
		typedStruct := &v3xdsxdstypepb.TypedStruct{}
		if err := config.UnmarshalTo(typedStruct); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal resource: %v", err)
		}
		return convertCustomConfig(typedStruct.TypeUrl, typedStruct.Value)
	}
	return nil, "", fmt.Errorf("Custom config not implemented for type %v", config.GetTypeUrl())
}

func convertCustomConfig(typeURL string, s *structpb.Struct) (json.RawMessage, string, error) {
	// The gRPC policy name will be the "type name" part of the value of the
	// type_url field in the TypedStruct. We get this by using the part after
	// the last / character. Can assume a valid type_url from the control plane.
	urls := strings.Split(typeURL, "/")
	name := urls[len(urls)-1]

	rawJSON, err := json.Marshal(s)
	if err != nil {
		return nil, "", fmt.Errorf("error converting custom lb policy %v: %v for %+v", err, typeURL, s)
	}
	return rawJSON, name, nil
}
