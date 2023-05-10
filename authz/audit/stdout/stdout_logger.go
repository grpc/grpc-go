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
	// Timestamp represents time when Log method prints the audit.Event
	Timestamp string `json:"timestamp"`
}

// StdoutLogger contains Log method to be invoked to log audit.Event.
type logger struct {
}

// Log marshals the audit.Event to json and prints it using log.go
func (logger *logger) Log(event *audit.Event) {
	jsonBytes, err := json.Marshal(convertEvent(event))
	if err != nil {
		grpcLogger.Errorf("failed to marshal AuditEvent data to JSON: %v", err)
	}
	log.Println(string(jsonBytes))
}

const (
	stdName = "stdout"
)

// LoggerConfig embeds audit.LoggerConfig
type LoggerConfig struct {
	audit.LoggerConfig
}

// StdoutLoggerBuilder contains information to build StdoutLogger
type loggerBuilder struct{}

// Name returns a hardcoded name of the StdoutLogger
func (loggerBuilder) Name() string {
	return stdName
}

// Build returns default StdoutLogger (audit.LoggerConfig is ignored)
func (*loggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &logger{}
}

// ParseLoggerConfig returns LoggerConfig (json.RawMessage is ignored)
func (*loggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	if config != nil {
		grpcLogger.Warningf("Config value %v ignored, StdoutLogger doesn't support custom configs", string(config))
	}
	return &LoggerConfig{}, nil
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
