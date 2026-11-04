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
	"errors"
	"testing"

	"google.golang.org/grpc/internal/binarylog"
)

type fakeMethodLogger struct{}

func (fakeMethodLogger) Log(_ context.Context, _ binarylog.LogEntryConfig) {
	// pass
}

// create clientStream with attempt == nil
func newClientStreamWithNilAttempt(ctx context.Context) *clientStream {
	return &clientStream{
		ctx:     ctx,
		attempt: nil,
		binlogs: []binarylog.MethodLogger{fakeMethodLogger{}},
	}
}

// Trailer() test with cs.attempt == nil
func (s) TestClientStream_Trailer_NilAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cs := newClientStreamWithNilAttempt(ctx)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Trailer() panicked with nil attempt: %v", r)
		}
	}()
	if md := cs.Trailer(); md != nil {
		t.Fatalf("Trailer() = %v, want nil when attempt is nil", md)
	}
}

// Context() test with cs.attempt == nil
func (s) TestClientStream_Context_NilAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cs := newClientStreamWithNilAttempt(ctx)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Context() panicked with nil attempt: %v", r)
		}
	}()
	if got := cs.Context(); got == nil {
		t.Fatal("Context() returned nil context")
	}
}

// regression test for finish() with attempt == nil and binlog
func (s) TestClientStream_Finish_NilAttempt_WithBinlog(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	cs := newClientStreamWithNilAttempt(ctx)
	cs.cancel = func() {}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("finish() panicked with nil attempt and binlog: %v", r)
		}
	}()
	cs.finish(errors.New("test error"))
}
