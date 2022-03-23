/*
 *
 * Copyright 2022 gRPC authors.
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

package observability

import (
	"reflect"
	"testing"
)

// TestGetCustomTags tests the normal tags parsing
func (s) TestGetCustomTags(t *testing.T) {
	var (
		input = []string{
			"GRPC_OBSERVABILITY_APP_NAME=app1",
			"GRPC_OBSERVABILITY_DATACENTER=us-west1-a",
			"GRPC_OBSERVABILITY_smallcase=OK",
		}
		expect = map[string]string{
			"APP_NAME":   "app1",
			"DATACENTER": "us-west1-a",
			"smallcase":  "OK",
		}
	)
	result := getCustomTags(input)
	if !reflect.DeepEqual(result, expect) {
		t.Errorf("result [%+v] != expect [%+v]", result, expect)
	}
}

// TestGetCustomTagsInvalid tests the invalid cases of tags parsing
func (s) TestGetCustomTagsInvalid(t *testing.T) {
	var (
		input = []string{
			"GRPC_OBSERVABILITY_APP_NAME=app1",
			"GRPC_OBSERVABILITY=foo",
			"GRPC_OBSERVABILITY_=foo", // Users should not set "" as key name
			"GRPC_STUFF=foo",
			"STUFF_GRPC_OBSERVABILITY_=foo",
		}
		expect = map[string]string{
			"APP_NAME": "app1",
		}
	)
	result := getCustomTags(input)
	if !reflect.DeepEqual(result, expect) {
		t.Errorf("result [%+v] != expect [%+v]", result, expect)
	}
}
