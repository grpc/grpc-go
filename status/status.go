/*
 *
 * Copyright 2017, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

// Package status implements errors returned by gRPC.  These errors are
// serialized and transmitted on the wire between server and client, and allow
// for additional data to be transmitted via the Details field in the status
// proto.  gRPC service handlers should return an error created by this
// package, and gRPC clients should expect a corresponding error to be
// returned from the RPC call.
//
// This package upholds the invariants that a non-nil error may not
// contain an OK code, and an OK code must result in a nil error.
package status

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	spb "github.com/google/go-genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
)

// Status provides access to grpc status details and is implemented by all
// errors returned from this package except nil errors, which are not typed.
// Note: gRPC users should not implement their own Statuses.  Custom data may
// be attached to the spb.Status proto's Details field.
type Status interface {
	// Code returns the status code.
	Code() codes.Code
	// Message returns the status message.
	Message() string
	// Proto returns a copy of the status in proto form.
	Proto() *spb.Status
	// Err returns an error representing the status.
	Err() error
}

// okStatus is a Status whose Code method returns codes.OK, but does not
// implement error.  To represent an OK code as an error, use an untyped nil.
type okStatus struct{}

func (okStatus) Code() codes.Code {
	return codes.OK
}

func (okStatus) Message() string {
	return ""
}

func (okStatus) Proto() *spb.Status {
	return nil
}

func (okStatus) Err() error {
	return nil
}

// statusError contains a status proto.  It is embedded and not aliased to
// allow for accessor functions of the same name.  It implements error and
// Status, and a nil statusError should never be returned by this package.
type statusError struct {
	*spb.Status
}

func (se *statusError) Error() string {
	return fmt.Sprintf("rpc error: code = %s desc = %s", se.Code(), se.Message())
}

func (se *statusError) Code() codes.Code {
	return codes.Code(se.Status.Code)
}

func (se *statusError) Message() string {
	return se.Status.Message
}

func (se *statusError) Proto() *spb.Status {
	return proto.Clone(se.Status).(*spb.Status)
}

func (se *statusError) Err() error {
	return se
}

// New returns a Status representing c and msg.
func New(c codes.Code, msg string) Status {
	if c == codes.OK {
		return okStatus{}
	}
	return &statusError{Status: &spb.Status{Code: int32(c), Message: msg}}
}

// Newf returns New(c, fmt.Sprintf(format, a...)).
func Newf(c codes.Code, format string, a ...interface{}) Status {
	return New(c, fmt.Sprintf(format, a...))
}

// Error returns an error representing c and msg.  If c is OK, returns nil.
func Error(c codes.Code, msg string) error {
	return New(c, msg).Err()
}

// Errorf returns Error(c, fmt.Sprintf(format, a...)).
func Errorf(c codes.Code, format string, a ...interface{}) error {
	return Error(c, fmt.Sprintf(format, a...))
}

// ErrorProto returns an error representing s.  If s.Code is OK, returns nil.
func ErrorProto(s *spb.Status) error {
	return FromProto(s).Err()
}

// FromProto returns a Status representing s.  If s.Code is OK, Message and
// Details may be lost.
func FromProto(s *spb.Status) Status {
	if s.GetCode() == int32(codes.OK) {
		return okStatus{}
	}
	return &statusError{Status: proto.Clone(s).(*spb.Status)}
}

// FromError returns a Status representing err if it was produced from this
// package, otherwise it returns nil, false.
func FromError(err error) (s Status, ok bool) {
	if err == nil {
		return okStatus{}, true
	}
	s, ok = err.(Status)
	return s, ok
}
