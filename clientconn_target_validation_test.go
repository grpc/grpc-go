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

	// Test with an invalid target (should fail).
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
}
