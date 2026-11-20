/*
 *
 * Copyright 2018 gRPC authors.
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
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// stubMethodLogger records how many binary log entries finish() emits.
type stubMethodLogger struct {
	logged int
}

func (s *stubMethodLogger) Log(context.Context, binarylog.LogEntryConfig) { s.logged++ }

// If a stream is torn down before its first attempt is created while a
// binary logger is attached, finish() must not log anything: the trailer
// path would panic on the nil attempt in Trailer(), and a cancel entry
// would be an orphan for a stream that never started. See
// https://github.com/grpc/grpc-go/issues/9481.
func (s) TestClientStream_Finish_NilAttemptWithBinlog(t *testing.T) {
	for _, err := range []error{errors.New("boom"), errContextCanceled} {
		logger := &stubMethodLogger{}
		ctx, cancel := context.WithCancel(context.Background())
		cs := &clientStream{
			binlogs: []binarylog.MethodLogger{logger},
			ctx:     ctx,
			cancel:  cancel,
		}
		// No attempt was ever created. Before the fix the non-cancel case
		// panicked inside Trailer() on the nil attempt, and the cancel case
		// emitted an orphan log entry for a stream that never started.
		cs.finish(err)
		if logger.logged != 0 {
			t.Fatalf("got %d binary log entries for a stream with no attempt, want 0", logger.logged)
		}
	}
}
