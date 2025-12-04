/*
 *
 * Copyright 2025 gRPC authors.
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
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
)

func TestWithTargetCheck(t *testing.T) {
	t.Run("success_passthrough", func(t *testing.T) {
		lis, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("net.Listen failed: %v", err)
		}
		defer lis.Close()

		go func() {
			conn, err := lis.Accept()
			if err == nil {
				conn.Close()
			}
		}()

		target := lis.Addr().String()

		conn, err := NewClient(target, WithTargetCheck(1*time.Second), WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("NewClient(%q) failed with WithTargetCheck: %v", target, err)
		}
		conn.Close()
	})

	t.Run("success_dns", func(t *testing.T) {
		lis, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("net.Listen failed: %v", err)
		}
		defer lis.Close()

		go func() {
			conn, err := lis.Accept()
			if err == nil {
				conn.Close()
			}
		}()

		target := "dns:///" + lis.Addr().String()

		conn, err := NewClient(target, WithTargetCheck(1*time.Second), WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("NewClient(%q) failed with WithTargetCheck: %v", target, err)
		}
		conn.Close()
	})

	t.Run("failure_unreachable", func(t *testing.T) {
		// Create a listener, get its address, then close it to ensure the port is closed.
		l, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			t.Fatalf("net.Listen failed: %v", err)
		}
		closedAddr := l.Addr().String()
		l.Close()

		_, err = NewClient(closedAddr, WithTargetCheck(100*time.Millisecond), WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			t.Fatalf("NewClient(%q) should have failed with WithTargetCheck pointing to closed port", closedAddr)
		}
	})

	t.Run("skip_other_schemes", func(t *testing.T) {
		// For schemes other than "dns" and "passthrough", validation should be skipped.
		// Use a unix scheme which won't cause validation.
		target := "unix:///tmp/nonexistent.sock"

		// This should succeed even though the socket doesn't exist because
		// validation is skipped for non-dns/passthrough schemes.
		conn, err := NewClient(target, WithTargetCheck(100*time.Millisecond), WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("NewClient(%q) with unix scheme should skip validation and succeed: %v", target, err)
		}
		conn.Close()
	})
}
