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
	"strings"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestHTTP2ClientRejectsInvalidAddressMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &http2Client{
		scheme:    "http",
		userAgent: "test",
		md:        metadata.MD{"bad key": {"value"}},
	}
	if _, err := c.createHeaderFields(ctx, &CallHdr{
		Method: "/grpc.testing.TestService/EmptyCall",
		Host:   "example.test",
	}); err == nil {
		t.Fatal("createHeaderFields() succeeded with invalid address metadata, want error")
	} else if !strings.Contains(err.Error(), "header") {
		t.Fatalf("createHeaderFields() returned error %q, want metadata validation error", err)
	}
}
