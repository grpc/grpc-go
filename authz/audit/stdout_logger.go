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

package audit

import (
	"encoding/json"
	"fmt"
	"time"

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

type StdOutLogger struct {
}

func (logger *StdOutLogger) Log(event *Event) {
	jsonBytes, err := json.Marshal(convertEvent(event)) //internal structure mimicking event with annotations how to marshall to json
	if err != nil {
		grpcLogger.Errorf("failed to marshal AuditEvent data to JSON: %v", err)
	}
	//message := fmt.Sprintf("[AuthZ Audit StdOutLogger] %s %v",
	//	time.Now().Format(time.RFC3339), string(jsonBytes))
	fmt.Println(string(jsonBytes)) // built in log.go
}

const (
	stdName = "stdout"
)

type StdoutLoggerConfig struct{}

func (StdoutLoggerConfig) loggerConfig() {}

type StdOutLoggerBuilder struct{}

func (StdOutLoggerBuilder) Name() string {
	return stdName
}

func (StdOutLoggerBuilder) Build(LoggerConfig) Logger {
	return &StdOutLogger{}
}

func (StdOutLoggerBuilder) ParseLoggerConfig(config json.RawMessage) (LoggerConfig, error) {
	grpcLogger.Warningf("Config value %v ignored, "+
		"StdOutLogger doesn't support custom configs", string(config))
	return &StdoutLoggerConfig{}, nil
}

func convertEvent(event *Event) StdoutEvent {
	return StdoutEvent{
		FullMethodName: event.FullMethodName,
		Principal:      event.Principal,
		PolicyName:     event.PolicyName,
		MatchedRule:    event.MatchedRule,
		Authorized:     event.Authorized,
		Timestamp:      time.Now().Format(time.RFC3339),
	}
}
