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
	"strings"
)

const (
	envPrefixCustomTags = "GRPC_OBSERVABILITY_"
	envPrefixLen        = len(envPrefixCustomTags)
)

func getCustomTags(envs []string) map[string]string {
	m := make(map[string]string)
	for _, e := range envs {
		if !strings.HasPrefix(e, envPrefixCustomTags) {
			continue
		}
		tokens := strings.SplitN(e, "=", 2)
		if len(tokens) == 2 {
			if len(tokens[0]) == envPrefixLen {
				// Empty key is not allowed
				continue
			}
			m[tokens[0][envPrefixLen:]] = tokens[1]
		}
	}
	return m
}
