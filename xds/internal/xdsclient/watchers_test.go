/*
 *
 * Copyright 2021 gRPC authors.
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
 */

package xdsclient

import (
	"testing"

	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
)

// findPubsubForTest returns the pubsub for the given authority, to send updates
// to. If authority is "", the default is returned. If the authority is not
// found, the test will fail.
func findPubsubForTest(t *testing.T, c *clientImpl, authority string) pubsub.UpdateHandler {
	t.Helper()
	var config *bootstrap.ServerConfig
	if authority == "" {
		config = c.config.XDSServer
	} else {
		authConfig, ok := c.config.Authorities[authority]
		if !ok {
			t.Fatalf("failed to find authority %q", authority)
		}
		config = authConfig.XDSServer
	}
	a := c.authorities[config.String()]
	if a == nil {
		t.Fatalf("authority for %q is not created", authority)
	}
	return a.pubsub
}
