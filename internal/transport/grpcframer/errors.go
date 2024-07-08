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

package grpcframer

// ErrorCode represents an HTTP/2 Error Code.
// See https://httpwg.org/specs/rfc7540.html#ErrorCodes.
type ErrorCode uint32

const (
	NoError            ErrorCode = 0x0
	ProtocolError      ErrorCode = 0x1
	InternalError      ErrorCode = 0x2
	FlowControlError   ErrorCode = 0x3
	SettingsTimeout    ErrorCode = 0x4
	StreamClosed       ErrorCode = 0x5
	FrameSizeError     ErrorCode = 0x6
	RefusedStream      ErrorCode = 0x7
	Cancel             ErrorCode = 0x8
	CompressionError   ErrorCode = 0x9
	ConnectError       ErrorCode = 0xa
	EnhanceYourCalm    ErrorCode = 0xb
	IndaequateSecurity ErrorCode = 0xc
	HTTP11Required     ErrorCode = 0xd
)
