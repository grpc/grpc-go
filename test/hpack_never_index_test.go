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

package test

import (
	"context"
	"net"
	"testing"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/experimental"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/metadata"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

// TestHPACKNeverIndexMetadataKeys verifies that metadata keys configured via
// experimental.WithHPACKNeverIndexMetadataKeys are sent as HPACK never-indexed
// literals, that keys are matched case-insensitively, that the last option
// wins, and that other headers stay indexable.
func (s) TestHPACKNeverIndexMetadataKeys(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen() failed: %v", err)
	}
	defer lis.Close()

	ctCh := testutils.NewChannel()
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("lis.Accept() failed: %v", err)
			return
		}
		ctCh.Send(newClientTester(t, conn))
	}()

	cc, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		experimental.WithHPACKNeverIndexMetadataKeys("x-stable"), // Overridden by the next option.
		experimental.WithHPACKNeverIndexMetadataKeys("X-Request-Id"),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient(%q) failed: %v", lis.Addr().String(), err)
	}
	defer cc.Close()
	cc.Connect()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	val, err := ctCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Timeout waiting for the client to connect: %v", err)
	}
	ct := val.(*clientTester)
	defer ct.conn.Close()
	ct.fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)

	// The server never responds, so the RPC only ends when rpcCtx is canceled.
	rpcCtx, rpcCancel := context.WithCancel(ctx)
	defer rpcCancel()
	rpcCtx = metadata.AppendToOutgoingContext(rpcCtx, "x-request-id", "abc-123", "x-stable", "stable-value")
	go testgrpc.NewTestServiceClient(cc).EmptyCall(rpcCtx, &testpb.Empty{})

	var hf *http2.MetaHeadersFrame
	for hf == nil {
		f, err := ct.fr.ReadFrame()
		if err != nil {
			t.Fatalf("Error reading frame from client: %v", err)
		}
		hf, _ = f.(*http2.MetaHeadersFrame)
	}

	wantSensitive := map[string]bool{
		"x-request-id": true,
		"x-stable":     false,
		"content-type": false,
	}
	for _, f := range hf.Fields {
		want, ok := wantSensitive[f.Name]
		if !ok {
			continue
		}
		delete(wantSensitive, f.Name)
		if f.Sensitive != want {
			t.Errorf("Header %q: Sensitive = %v, want %v", f.Name, f.Sensitive, want)
		}
	}
	for name := range wantSensitive {
		t.Errorf("Header %q was not sent", name)
	}
}
