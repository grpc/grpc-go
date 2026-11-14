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

// stubMethodLogger records how many binary log entries finish() emits.
type stubMethodLogger struct {
	logged int
}

func (s *stubMethodLogger) Log(context.Context, binarylog.LogEntryConfig) { s.logged++ }

// If a stream is torn down before its first attempt is created while a
// binary logger is attached, finish() must not emit a log entry: Trailer()
// and Context() both dereference cs.attempt, and there is no attempt to log
// about. See https://github.com/grpc/grpc-go/issues/9481.
func (s) TestClientStream_Finish_NilAttemptWithBinlog(t *testing.T) {
	logger := &stubMethodLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	cs := &clientStream{
		binlogs: []binarylog.MethodLogger{logger},
		ctx:     ctx,
		cancel:  cancel,
	}
	// No attempt was ever created; finish with a non-cancel error. Before
	// the fix this panicked inside Trailer() on the nil attempt.
	cs.finish(errors.New("boom"))
	if logger.logged != 0 {
		t.Fatalf("expected no binary log entries for a stream with no attempt, got %d", logger.logged)
	}
}
