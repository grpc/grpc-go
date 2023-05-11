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
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/authz/audit"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestStdoutLogger_Log(t *testing.T) {
	tests := map[string]struct {
		event       *audit.Event
		wantMessage string
		wantErr     string
	}{
		"few fields": {
			event:       &audit.Event{PolicyName: "test policy", Principal: "test principal"},
			wantMessage: `{"fullMethodName":"","principal":"test principal","policyName":"test policy","matchedRule":"","authorized":false`,
		},
		"all fields": {
			event: &audit.Event{
				FullMethodName: "/helloworld.Greeter/SayHello",
				Principal:      "spiffe://example.org/ns/default/sa/default/backend",
				PolicyName:     "example-policy",
				MatchedRule:    "dev-access",
				Authorized:     true,
			},
			wantMessage: `{"fullMethodName":"/helloworld.Greeter/SayHello",` +
				`"principal":"spiffe://example.org/ns/default/sa/default/backend","policyName":"example-policy",` +
				`"matchedRule":"dev-access","authorized":true`,
		},
	}

	content := json.RawMessage(`{"name": "conf", "val": "to be ignored"}`)
	builder := &loggerBuilder{}
	config, _ := builder.ParseLoggerConfig(content)
	auditLogger := builder.Build(config)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			auditLogger.Log(test.event)
			var e event
			if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
				t.Fatalf("Failed to unmarshal audit log event: %v", err)
			}
			if strings.TrimSpace(e.Timestamp) == "" {
				t.Fatalf("Resulted event has no timestamp: %v", e)
			}

			if diff := cmp.Diff(trimEvent(e), test.event); diff != "" {
				t.Fatalf("Unexpected message\ndiff (-got +want):\n%s", diff)
			}
			buf.Reset()
		})
	}
}

func (s) TestStdoutLoggerBuilder_NilConfig(t *testing.T) {
	builder := &loggerBuilder{}
	config, err := builder.ParseLoggerConfig(nil)
	if err != nil {
		t.Fatalf("Failed to parse stdout logger configuration: %v", err)
	}
	if l := builder.Build(config); l == nil {
		t.Fatal("Failed to build stdout audit logger")
	}
}

// trimEvent converts a logged stdout.event into an audit.Event
// by removing Timestamp field. It is used for comparing events during testing.
func trimEvent(testEvent event) *audit.Event {
	return &audit.Event{
		FullMethodName: testEvent.FullMethodName,
		Principal:      testEvent.Principal,
		PolicyName:     testEvent.PolicyName,
		MatchedRule:    testEvent.MatchedRule,
		Authorized:     testEvent.Authorized,
	}
}
