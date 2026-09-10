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
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s) TestServerStreamSetSendCompressReturnsCloseStatus(t *testing.T) {
	for _, test := range []struct {
		name string
		code codes.Code
	}{
		{name: "canceled", code: codes.Canceled},
		{name: "deadline_exceeded", code: codes.DeadlineExceeded},
		{name: "internal", code: codes.Internal},
		{name: "resource_exhausted", code: codes.ResourceExhausted},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := &ServerStream{}
			stream.setCloseStatus(status.New(test.code, "stream closed"))

			err := stream.SetSendCompress("gzip")
			if got := status.Code(err); got != test.code {
				t.Fatalf("SetSendCompress() returned code %v, want %v: %v", got, test.code, err)
			}
		})
	}
}

func (s) TestServerStreamSetSendCompressAfterFinishedStream(t *testing.T) {
	stream := &ServerStream{Stream: Stream{state: streamDone}}

	err := stream.SetSendCompress("gzip")
	if got, want := err.Error(), errSetSendCompressorTooLate; got != want {
		t.Fatalf("SetSendCompress() returned %q, want %q", got, want)
	}
	if _, ok := status.FromError(err); ok {
		t.Fatalf("SetSendCompress() returned status error %v, want ordinary stream-done error", err)
	}
}

func (s) TestServerStreamSetSendCompressUsesFirstCloseStatus(t *testing.T) {
	for _, test := range []struct {
		name       string
		firstCode  codes.Code
		secondCode codes.Code
	}{
		{
			name:       "internal_before_cancellation",
			firstCode:  codes.Internal,
			secondCode: codes.Canceled,
		},
		{
			name:       "cancellation_before_internal",
			firstCode:  codes.Canceled,
			secondCode: codes.Internal,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := &ServerStream{}
			stream.setCloseStatus(status.New(test.firstCode, "first close"))
			stream.setCloseStatus(status.New(test.secondCode, "second close"))

			err := stream.SetSendCompress("gzip")
			if got := status.Code(err); got != test.firstCode {
				t.Fatalf("SetSendCompress() returned code %v, want first close code %v: %v", got, test.firstCode, err)
			}
		})
	}
}

func (s) TestServerStreamSetSendCompressPrefersHeadersSent(t *testing.T) {
	stream := &ServerStream{Stream: Stream{state: streamDone}}
	stream.headerSent.Store(true)
	stream.setCloseStatus(status.New(codes.Canceled, context.Canceled.Error()))

	err := stream.SetSendCompress("gzip")
	if got, want := err.Error(), errSetSendCompressorTooLate; got != want {
		t.Fatalf("SetSendCompress() returned %q, want %q", got, want)
	}
	if _, ok := status.FromError(err); ok {
		t.Fatalf("SetSendCompress() returned status error %v, want ordinary headers-sent error", err)
	}
}
