/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/testutils/xds/bootstrap"
)

// Test that multiple New() returns the same Client. And only when the last
// client is closed, the underlying client is closed.
func (s) TestClientNewSingleton(t *testing.T) {
	// Create a bootstrap configuration, place it in a file in the temp
	// directory, and set the bootstrap env vars to point to it.
	nodeID := uuid.New().String()
	cleanup, err := bootstrap.CreateFile(bootstrap.Options{
		NodeID:    nodeID,
		ServerURI: "non-existent-server-address",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	// Override the singleton creation hook to get notified.
	origSingletonClientImplCreateHook := singletonClientImplCreateHook
	singletonCreationCh := testutils.NewChannel()
	singletonClientImplCreateHook = func() {
		singletonCreationCh.Replace(nil)
	}
	defer func() { singletonClientImplCreateHook = origSingletonClientImplCreateHook }()

	// Override the singleton close hook to get notified.
	origSingletonClientImplCloseHook := singletonClientImplCloseHook
	singletonCloseCh := testutils.NewChannel()
	singletonClientImplCloseHook = func() {
		singletonCloseCh.Replace(nil)
	}
	defer func() { singletonClientImplCloseHook = origSingletonClientImplCloseHook }()

	// The first call to New() should create a new singleton client.
	_, closeFunc, err := New()
	if err != nil {
		t.Fatalf("failed to create xDS client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := singletonCreationCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for singleton xDS client to be created: %v", err)
	}

	// Calling New() again should not create new singleton client implementations.
	const count = 9
	closeFuncs := make([]func(), 9)
	for i := 0; i < count; i++ {
		func() {
			_, closeFuncs[i], err = New()
			if err != nil {
				t.Fatalf("%d-th call to New() failed with error: %v", i, err)
			}

			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := singletonCreationCh.Receive(sCtx); err == nil {
				t.Fatalf("%d-th call to New() created a new singleton client", i)
			}
		}()
	}

	// Call Close() multiple times on each of the clients created in the above for
	// loop. Close() calls are idempotent, and the underlying client
	// implementation will not be closed until we release the first reference we
	// acquired above, via the first call to New().
	for i := 0; i < count; i++ {
		func() {
			closeFuncs[i]()
			closeFuncs[i]()

			sCtx, sCancel := context.WithTimeout(ctx, defaultTestShortTimeout)
			defer sCancel()
			if _, err := singletonCloseCh.Receive(sCtx); err == nil {
				t.Fatal("singleton client implementation closed before all references are released")
			}
		}()
	}

	// Call the last Close(). The underlying implementation should be closed.
	closeFunc()
	if _, err := singletonCloseCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout waiting for singleton client implementation to be closed: %v", err)
	}

	// Calling New() again, after the previous Client was actually closed, should
	// create a new one.
	_, closeFunc, err = New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer closeFunc()
	if _, err := singletonCreationCh.Receive(ctx); err != nil {
		t.Fatalf("Timeout when waiting for singleton xDS client to be created: %v", err)
	}
}
