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

package grpchttp2

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestErrorCodeString(t *testing.T) {
	errCodesTest := []struct {
		err  ErrCode
		want string
	}{
		{ErrCodeNoError, "NO_ERROR"},
		{ErrCodeProtocol, "PROTOCOL_ERROR"},
		{ErrCodeInternal, "INTERNAL_ERROR"},
		{ErrCodeFlowControl, "FLOW_CONTROL_ERROR"},
		{ErrCodeSettingsTimeout, "SETTINGS_TIMEOUT"},
		{ErrCodeStreamClosed, "STREAM_CLOSED"},
		{ErrCodeFrameSize, "FRAME_SIZE_ERROR"},
		{ErrCodeRefusedStream, "REFUSED_STREAM"},
		{ErrCodeCancel, "CANCEL"},
		{ErrCodeCompression, "COMPRESSION_ERROR"},
		{ErrCodeConnect, "CONNECT_ERROR"},
		{ErrCodeEnhanceYourCalm, "ENHANCE_YOUR_CALM"},
		{ErrCodeIndaequateSecurity, "INADEQUATE_SECURITY"},
		{ErrCodeHTTP11Required, "HTTP_1_1_REQUIRED"},
		{ErrCode(0x1), "PROTOCOL_ERROR"},
		{ErrCode(0xf), "unknown error code 0xf"},
	}

	for _, errTest := range errCodesTest {
		if errTest.err.String() != errTest.want {
			t.Errorf("got %q, want %q", errTest.err.String(), errTest.want)
		}
	}
}
