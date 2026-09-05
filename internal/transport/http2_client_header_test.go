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

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

// TestCreateHeaderFieldsGrpcTimeoutNeverIndexed verifies that grpc-timeout,
// whose value is unique on essentially every RPC, is marked sensitive so the
// HPACK encoder does not add it to its dynamic table, while ordinary reusable
// headers remain indexable.
func (s) TestCreateHeaderFieldsGrpcTimeoutNeverIndexed(t *testing.T) {
	tr := &http2Client{
		scheme:    "https",
		userAgent: "grpc-go/test",
		md:        metadata.MD{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.MD{
		"x-user-header": []string{"stable-value"},
	})
	callHdr := &CallHdr{
		Method: "/grpc.testing.TestService/UnaryCall",
		Host:   "server:443",
	}

	hf, err := tr.createHeaderFields(ctx, callHdr)
	if err != nil {
		t.Fatalf("createHeaderFields() failed: %v", err)
	}

	// wantSensitive maps a header name to whether it must be marked sensitive.
	wantSensitive := map[string]bool{
		"grpc-timeout":  true,  // remaining-time countdown, unique per RPC
		"x-user-header": false, // ordinary reusable metadata
		"content-type":  false, // reusable reserved header
		":path":         false, // reusable across calls to the same method
	}

	seen := map[string]bool{}
	for _, f := range hf {
		want, ok := wantSensitive[f.Name]
		if !ok {
			continue
		}
		seen[f.Name] = true
		if f.Sensitive != want {
			t.Errorf("header %q: Sensitive = %v, want %v", f.Name, f.Sensitive, want)
		}
	}
	for name := range wantSensitive {
		if !seen[name] {
			t.Errorf("expected header %q was not produced", name)
		}
	}
}
