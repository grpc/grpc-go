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

	"google.golang.org/grpc/credentials"
)

type invalidMetadataCreds struct {
	md map[string]string
}

func (c invalidMetadataCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return c.md, nil
}

func (invalidMetadataCreds) RequireTransportSecurity() bool { return false }

func TestPerRPCCredentialMetadataIsValidated(t *testing.T) {
	tests := []struct {
		name string
		md   map[string]string
	}{
		{
			name: "invalid key",
			md:   map[string]string{"bad key": "value"},
		},
		{
			name: "invalid value",
			md:   map[string]string{"valid-key": "bad\x01value"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &http2Client{
				scheme:      "http",
				userAgent:   "test",
				perRPCCreds: []credentials.PerRPCCredentials{invalidMetadataCreds{md: test.md}},
			}
			if _, err := c.createHeaderFields(ctx, &CallHdr{
				Method: "/grpc.testing.TestService/EmptyCall",
				Host:   "example.test",
			}); err == nil {
				t.Fatal("createHeaderFields() succeeded with invalid per-RPC credential metadata, want error")
			} else if !strings.Contains(err.Error(), "header") {
				t.Fatalf("createHeaderFields() returned error %q, want metadata validation error", err)
			}
		})
	}
}

func TestCallCredentialMetadataIsValidated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &http2Client{
		scheme:    "http",
		userAgent: "test",
	}
	if _, err := c.createHeaderFields(ctx, &CallHdr{
		Method: "/grpc.testing.TestService/EmptyCall",
		Host:   "example.test",
		Creds:  invalidMetadataCreds{md: map[string]string{"bad key": "value"}},
	}); err == nil {
		t.Fatal("createHeaderFields() succeeded with invalid call credential metadata, want error")
	} else if !strings.Contains(err.Error(), "header") {
		t.Fatalf("createHeaderFields() returned error %q, want metadata validation error", err)
	}
}
