/*
 *
 * Copyright 2024 gRPC authors.
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

package grpchttp2_test

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/transport/grpchttp2"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestErrorCodeString(t *testing.T) {
	errCodesTest := []struct {
		err  grpchttp2.ErrCode
		want string
	}{
		{grpchttp2.ErrCodeNoError, "NO_ERROR"},
		{grpchttp2.ErrCodeProtocol, "PROTOCOL_ERROR"},
		{grpchttp2.ErrCodeInternal, "INTERNAL_ERROR"},
		{grpchttp2.ErrCodeFlowControl, "FLOW_CONTROL_ERROR"},
		{grpchttp2.ErrCodeSettingsTimeout, "SETTINGS_TIMEOUT"},
		{grpchttp2.ErrCodeStreamClosed, "STREAM_CLOSED"},
		{grpchttp2.ErrCodeFrameSize, "FRAME_SIZE_ERROR"},
		{grpchttp2.ErrCodeRefusedStream, "REFUSED_STREAM"},
		{grpchttp2.ErrCodeCancel, "CANCEL"},
		{grpchttp2.ErrCodeCompression, "COMPRESSION_ERROR"},
		{grpchttp2.ErrCodeConnect, "CONNECT_ERROR"},
		{grpchttp2.ErrCodeEnhanceYourCalm, "ENHANCE_YOUR_CALM"},
		{grpchttp2.ErrCodeIndaequateSecurity, "INADEQUATE_SECURITY"},
		{grpchttp2.ErrCodeHTTP11Required, "HTTP_1_1_REQUIRED"},
		{grpchttp2.ErrCode(0x1), "PROTOCOL_ERROR"},
		{grpchttp2.ErrCode(0xf), "unknown error code 0xf"},
	}

	for _, errTest := range errCodesTest {
		if errTest.err.String() != errTest.want {
			t.Errorf("got %q, want %q", errTest.err.String(), errTest.want)
		}
	}
}
