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

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Build a separate program because the test binary itself uses gRPC servers.
// Check symbols instead of binary size, which varies across Go versions and
// platforms.
func (s) TestClientOnlyBinaryDoesNotLinkServer(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("Go tool unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "client")
	// Override any linker flags in GOFLAGS so the symbol table is preserved.
	cmd := exec.CommandContext(ctx, goTool, "build", "-ldflags=", "-o", binary, "testdata/client_only/main.go")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Building client-only binary failed: %v\n%s", err, out)
	}
	out, err := exec.CommandContext(ctx, goTool, "tool", "nm", binary).CombinedOutput()
	if err != nil {
		t.Fatalf("Reading client-only binary symbols failed: %v\n%s", err, out)
	}
	var hasClient bool
	for line := range strings.SplitSeq(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		symbol := fields[len(fields)-1]
		if strings.HasPrefix(symbol, "google.golang.org/grpc.(*ClientConn).") {
			hasClient = true
		}
		if strings.HasPrefix(symbol, "google.golang.org/grpc.(*Server).") {
			t.Errorf("Client-only binary contains server method: %s", symbol)
		}
	}
	if !hasClient {
		t.Fatal("Client-only binary contains no ClientConn methods")
	}
}
