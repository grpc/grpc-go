/*
 *
 * Copyright 2026 gRPC authors.
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

package grpc

import "testing"

// FuzzParseServiceConfig fuzzes the service config JSON parser. Service config
// JSON can be supplied by resolvers (for example, the DNS resolver reads it
// from TXT records when the TXT service config experiment is enabled) or set
// as the default service config, so the parser must tolerate arbitrary input
// without panicking or hanging. Parse errors are expected and are returned to
// the caller as an error.
func FuzzParseServiceConfig(f *testing.F) {
	// Note: seeds must not make the parser emit warning logs; the test
	// framework fails tests that log. Malformed inputs are left to the
	// fuzzer's exploration.
	f.Add(`{}`)
	f.Add(`{"loadBalancingConfig":[{"round_robin":{}}]}`)
	f.Add(`{"loadBalancingConfig":[{"round_robin":{}},{"pick_first":{}}],"methodConfig":[{"name":[{"service":"svc","method":"m"}],"waitForReady":true,"timeout":"1s"}]}`)
	f.Add(`{"methodConfig":[{"name":[{}],"retryPolicy":{"maxAttempts":4,"initialBackoff":"0.1s","maxBackoff":"1s","backoffMultiplier":2,"retryableStatusCodes":["UNAVAILABLE","DEADLINE_EXCEEDED"]}}]}`)
	f.Add(`{"healthCheckConfig":{"serviceName":"svc"}}`)
	f.Add(`{"loadBalancingConfig":[{"not_a_registered_policy":{}}]}`)
	f.Add(`null`)

	f.Fuzz(func(_ *testing.T, js string) {
		_ = parseServiceConfig(js, defaultMaxCallAttempts)
	})
}
