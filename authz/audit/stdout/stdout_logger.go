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

package stdout

import (
	"encoding/json"
	"log"
	"time"

	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/grpclog"
)

var grpcLogger = grpclog.Component("authz-audit")

type StdoutEvent struct {
	FullMethodName string `json:"fullMethodName"`
	Principal      string `json:"principal"`
	PolicyName     string `json:"policyName"`
	MatchedRule    string `json:"matchedRule"`
	Authorized     bool   `json:"authorized"`
	//Timestamp is populated using time.Now() during StdoutLogger.Log call
	Timestamp string `json:"timestamp"`
}

type StdoutLogger struct {
}

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

type StdoutLoggerConfig struct{}

func (StdoutLoggerConfig) LoggerConfig() {}

type StdoutLoggerBuilder struct{}

func (StdoutLoggerBuilder) Name() string {
	return stdName
}

func (StdoutLoggerBuilder) Build(audit.LoggerConfig) audit.Logger {
	return &StdoutLogger{}
}

func (StdoutLoggerBuilder) ParseLoggerConfig(config json.RawMessage) (audit.LoggerConfig, error) {
	grpcLogger.Warningf("Config value %v ignored, "+
		"StdoutLogger doesn't support custom configs", string(config))
	return &StdoutLoggerConfig{}, nil
}

func convertEvent(event *audit.Event) StdoutEvent {
	return StdoutEvent{
		FullMethodName: event.FullMethodName,
		Principal:      event.Principal,
		PolicyName:     event.PolicyName,
		MatchedRule:    event.MatchedRule,
		Authorized:     event.Authorized,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
}
