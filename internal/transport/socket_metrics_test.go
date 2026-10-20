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

package transport

import "testing"

// TestGetOutFlowWindowBeforeControlBufferInit verifies that querying the
// out flow control window before the control buffer exists (possible when a
// channelz GetSocket call races transport construction, see #9539) reports
// the window as unavailable instead of panicking on a nil control buffer.
func TestGetOutFlowWindowBeforeControlBufferInit(t *testing.T) {
	if got := (&http2Client{}).getOutFlowWindow(); got != -2 {
		t.Errorf("http2Client.getOutFlowWindow() with nil control buffer = %v, want -2", got)
	}
	if got := (&http2Server{}).getOutFlowWindow(); got != -2 {
		t.Errorf("http2Server.getOutFlowWindow() with nil control buffer = %v, want -2", got)
	}
}
