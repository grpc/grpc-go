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
	"testing"
)

var (
	builder = &StdOutLoggerBuilder{}
	logger  = builder.Build(nil)
)

func TestStdOutLogger_Log(t *testing.T) {
	event := &Event{PolicyName: "test policy", Principal: "test principal"}
	logger.Log(event)
}

//func TestMyLogger_ToJSON(t *testing.T) {
//	jsonBytes, err := logger.ToJSON()
//	if err != nil {
//		t.Fatalf("Failed to marshal logger to JSON: %v", err)
//	}
//
//	var restored StdOutLogger
//	err = json.Unmarshal(jsonBytes, &restored)
//	if err != nil {
//		t.Fatalf("Failed to unmarshal logger back from JSON: %v", err)
//	}
//
//	if !reflect.DeepEqual(logger, &restored) {
//		t.Errorf("ToJSON() test failed, restored = %v, want %v", restored, logger)
//	}
//}
