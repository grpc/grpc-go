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

type StdOutLogger struct {
}

func (logger *StdOutLogger) Log(event *Event) {
	jsonBytes, err := json.Marshal(event)
	if err != nil {
		grpcLogger.Errorf("failed to marshal AuditEvent data to JSON: %v", err)
	}
	message := fmt.Sprintf("[AuthZ Audit StdOutLogger] %s %v",
		time.Now().Format(time.RFC3339), string(jsonBytes))
	fmt.Println(message)
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
