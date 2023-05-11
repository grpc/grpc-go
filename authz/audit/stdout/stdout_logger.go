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

// Package stdout defines an stdout audit logger.
package stdout

import (
	"encoding/json"
	"log"
	"time"

	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/grpclog"
)

var grpcLogger = grpclog.Component("authz-audit")

func init() {
	audit.RegisterLoggerBuilder(&loggerBuilder{})
}

type event struct {
	FullMethodName string `json:"fullMethodName"`
	Principal      string `json:"principal"`
	PolicyName     string `json:"policyName"`
	MatchedRule    string `json:"matchedRule"`
	Authorized     bool   `json:"authorized"`
	Timestamp      string `json:"timestamp"` // Time when the audit event is logged via Log method
}

// logger implements the audit.Logger interface by logging to standard output.
type logger struct {
}

// Log marshals the audit.Event to json and prints it using log.go
func (logger *logger) Log(event *audit.Event) {
	jsonBytes, err := json.Marshal(convertEvent(event))
	if err != nil {
		grpcLogger.Errorf("failed to marshal AuditEvent data to JSON: %v", err)
		return
	}
	log.Println(string(jsonBytes))
}

const (
	stdName = "stdout"
)

// loggerConfig represents the configuration for the stdout logger.
// It is currently empty and implements the audit.Logger interface by embedding it.
type loggerConfig struct {
	audit.LoggerConfig
}

type loggerBuilder struct{}

func (loggerBuilder) Name() string {
	return stdName
}

// Build returns a new instance of the stdout logger.
// Passed in configuration is ignored as the stdout logger does not
// expect any configuration to be provided.
func (*loggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &logger{}
}

// ParseLoggerConfig is a no-op since the stdout logger does not accept any configuration.
func (*loggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	if config != nil {
		grpcLogger.Warningf("Stdout logger doesn't support custom configs. Ignoring:\n%s", string(config))
	}
	return &loggerConfig{}, nil
}

func convertEvent(auditEvent *audit.Event) *event {
	return &event{
		FullMethodName: auditEvent.FullMethodName,
		Principal:      auditEvent.Principal,
		PolicyName:     auditEvent.PolicyName,
		MatchedRule:    auditEvent.MatchedRule,
		Authorized:     auditEvent.Authorized,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
}
