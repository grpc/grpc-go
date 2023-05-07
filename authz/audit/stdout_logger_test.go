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
	"bytes"
	"encoding/json"
	"log"
	"testing"
	"time"
)

var (
	content     = json.RawMessage(`{"name": "conf", "val": "to be ignored"}`)
	builder     = &StdOutLoggerBuilder{}
	config, _   = builder.ParseLoggerConfig(content)
	auditLogger = builder.Build(config)
)

func TestStdOutLogger_Log(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	event := &Event{PolicyName: "test policy", Principal: "test principal"}
	auditLogger.Log(event)

	expected := `{"fullMethodName":"","principal":"test principal","policyName":"test policy","matchedRule":"","authorized":false`
	if buf.String() != (expected + ",\"timestamp\":\"" + time.Now().Format(time.RFC3339) + "\"}\n") {
		t.Fatalf("unexpected error\nwant:%v\n got:%v", expected, buf.String())
	}
}

func TestStdOutLogger_LogAllEventFields(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	event := &Event{
		FullMethodName: "/helloworld.Greeter/SayHello",
		Principal:      "spiffe://example.org/ns/default/sa/default/backend",
		PolicyName:     "example-policy",
		MatchedRule:    "dev-access",
		Authorized:     true,
	}
	auditLogger.Log(event)

	expected := `{"fullMethodName":"/helloworld.Greeter/SayHello",` +
		`"principal":"spiffe://example.org/ns/default/sa/default/backend","policyName":"example-policy",` +
		`"matchedRule":"dev-access","authorized":true`
	if buf.String() != (expected + ",\"timestamp\":\"" + time.Now().Format(time.RFC3339) + "\"}\n") {
		t.Fatalf("unexpected error\nwant:%v\n got:%v", expected, buf.String())
	}
}
