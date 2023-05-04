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
	"io"
	"log"
	"os"
	"testing"
)

var (
	content     = json.RawMessage(`{"name": "conf", "val": "to be ignored"}`)
	builder     = &StdOutLoggerBuilder{}
	config, _   = builder.ParseLoggerConfig(content)
	auditLogger = builder.Build(config)
)

func TestStdOutLogger_Log(t *testing.T) {
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	event := &Event{PolicyName: "test policy", Principal: "test principal"}
	auditLogger.Log(event)

	w.Close()
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	if err != nil {
		log.Fatal(err)
	}
	expected := `[AuthZ Audit StdOutLogger] {"FullMethodName":"","Principal":"test principal","PolicyName":"test policy","MatchedRule":"","Authorized":false}`
	if buf.String() != (expected + "\n") {
		t.Fatalf("unexpected error\nwant:%v\n got:%v", expected, buf.String())
	}
	os.Stdout = orig
}
