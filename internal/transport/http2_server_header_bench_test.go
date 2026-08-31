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
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// BenchmarkServerHeaderFields measures header-slice construction as the amount
// metadata varies in.
func BenchmarkServerHeaderFields(b *testing.B) {
	for _, mdCount := range []int{0, 4, 12} {
		b.Run(fmt.Sprintf("writeHeaderLocked_mdCount=%d", mdCount), func(b *testing.B) {
			t, s, cleanup := newBenchStream(b, mdCount, 0)
			defer cleanup()

			b.ReportAllocs()
			for b.Loop() {
				s.state = streamActive
				if err := t.writeHeaderLocked(s); err != nil {
					b.Fatal(err)
				}
			}
		})

		st := status.New(codes.OK, "OK")

		b.Run(fmt.Sprintf("writeStatus_mdCount=%d", mdCount), func(b *testing.B) {
			t, s, cleanup := newBenchStream(b, 0, mdCount)
			defer cleanup()

			b.ReportAllocs()
			for b.Loop() {
				s.state = streamActive
				s.headerSent.Store(false)
				if err := t.writeStatus(s, st); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func makeMD(prefix string, count int) metadata.MD {
	md := make(metadata.MD, count)
	for i := range count {
		md[fmt.Sprintf("%s-%d", prefix, i)] = []string{
			fmt.Sprintf("value-%d-a", i),
			fmt.Sprintf("value-%d-b", i),
		}
	}
	return md
}

func newBenchStream(b *testing.B, headerCount, trailerCount int) (*http2Server, *ServerStream, func()) {
	b.Helper()

	done := make(chan struct{})
	t := &http2Server{
		controlBuf:            newControlBuffer(done),
		setResetPingStrikes:   func() {},
		maxSendHeaderListSize: nil,
	}

	// Drain the control buffer continuously, the way loopyWriter would in a
	// real server. Without this, every headerFrame pushed by
	// writeHeaderLocked/writeStatus stays queued forever, so the benchmark
	// leaks memory and its timings are dominated by growing GC pressure
	// instead of the cost of the functions under test.
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			if _, err := t.controlBuf.get(true); err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	s := &ServerStream{
		Stream: Stream{
			id:             1,
			fc:             inFlow{limit: 65536},
			ctx:            ctx,
			contentSubtype: "proto",
		},
		st:     t,
		header: makeMD("header", headerCount),
	}

	s.trailer = makeMD("trailer", trailerCount)
	s.cancel = cancel

	cleanup := func() {
		cancel()
		close(done) // unblocks controlBuf.get so the drain goroutine exits
		<-drainDone
	}
	return t, s, cleanup
}