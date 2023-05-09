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

// Package stdout contains a Stdout implementation of audit logging interface
// The Stdoutlogger prints the audit.Event using log.go in json format
package stdout

import (
	"encoding/json"
	"log"
	"time"

	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/grpclog"
)

var grpcLogger = grpclog.Component("authz-audit")

type stdoutEvent struct {
	FullMethodName string `json:"fullMethodName"`
	Principal      string `json:"principal"`
	PolicyName     string `json:"policyName"`
	MatchedRule    string `json:"matchedRule"`
	Authorized     bool   `json:"authorized"`
	//Timestamp is populated using time.Now() during StdoutLogger.Log call
	Timestamp string `json:"timestamp"`
}

// StdoutLogger is an impl of audit.Logger which
// prints the audit.Event using log.go in json format
type StdoutLogger struct {
}

// Log implements audit.Logger.Log
// and prints the audit.Event using log.go in json format
func (logger *StdoutLogger) Log(event *audit.Event) {
	jsonBytes, err := json.Marshal(convertEvent(event))
	if err != nil {
		grpcLogger.Errorf("failed to marshal AuditEvent data to JSON: %v", err)
	}
	log.Println(string(jsonBytes))
}

const (
	stdName = "stdout"
)

// StdoutLoggerConfig implements audit.LoggerConfig
// Since the logger doesn't support custom configs, it's a no-op one
type StdoutLoggerConfig struct {
	audit.LoggerConfig
}

// StdoutLoggerBuilder provides stdout implementation of
// audit.LoggerBuilder
type StdoutLoggerBuilder struct{}

// Name implements audit.LoggerBuilder.Name and returns
// a hardcoded name of the StdoutLogger
func (StdoutLoggerBuilder) Name() string {
	return stdName
}

// Build implements audit.LoggerBuilder.Build
// LoggerConfig is ignored so it always returns default StdoutLogger
func (*StdoutLoggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &StdoutLogger{}
}

// ParseLoggerConfig implements audit.LoggerBuilder.ParseLoggerConfig
// Passed value is ignored but warning is printed
func (*StdoutLoggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	if config != nil {
		grpcLogger.Warningf("Config value %v ignored, "+
			"StdoutLogger doesn't support custom configs", string(config))
	}
	return &StdoutLoggerConfig{}, nil
}

func convertEvent(event *audit.Event) *stdoutEvent {
	return &stdoutEvent{
		FullMethodName: event.FullMethodName,
		Principal:      event.Principal,
		PolicyName:     event.PolicyName,
		MatchedRule:    event.MatchedRule,
		Authorized:     event.Authorized,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
}
