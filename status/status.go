/*
 *
 * Copyright 2017 gRPC authors.
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
	"context"
	"fmt"

	"google.golang.org/grpc/internal/status"

	spb "google.golang.org/genproto/googleapis/rpc/status"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/status/statustype"
)

func init() {
	internal.StatusRawProto = statusRawProto
}

func statusRawProto(s *Status) *spb.Status { return s.Proto() }

type Status = statustype.Status

// New returns a Status representing c and msg.
func New(c codes.Code, msg string) *Status {
	return statustype.New(c, msg)
}

// Newf returns New(c, fmt.Sprintf(format, a...)).
func Newf(c codes.Code, format string, a ...interface{}) *Status {
	return statustype.Newf(c, fmt.Sprintf(format, a...))
}

// Error returns an error representing c and msg.  If c is OK, returns nil.
func Error(c codes.Code, msg string) error {
	return statustype.New(c, msg).Err()
}

// Errorf returns Error(c, fmt.Sprintf(format, a...)).
func Errorf(c codes.Code, format string, a ...interface{}) error {
	return (*status.StatusError)(statustype.Newf(c, format, a...).Proto())
}

// ErrorProto returns an error representing s.  If s.Code is OK, returns nil.
func ErrorProto(s *spb.Status) error {
	return statustype.FromProto(s).Err()
}

// FromProto returns a Status representing s.
func FromProto(s *spb.Status) *Status {
	return statustype.FromProto(s)
}

// FromError returns a Status representing err if it was produced from this
// package or has a method `GRPCStatus() *Status`. Otherwise, ok is false and a
// Status is returned with codes.Unknown and the original error message.
func FromError(err error) (s *Status, ok bool) {
	if err == nil {
		return nil, true
	}
	if se, ok := err.(interface {
		GRPCStatus() *spb.Status
	}); ok {
		s := se.GRPCStatus()
		return FromProto(s), true
	}
	return New(codes.Unknown, err.Error()), false
}

// Convert is a convenience function which removes the need to handle the
// boolean return value from FromError.
func Convert(err error) *Status {
	s, _ := FromError(err)
	return s
}

// Code returns the Code of the error if it is a Status error, codes.OK if err
// is nil, or codes.Unknown otherwise.
func Code(err error) codes.Code {
	// Don't use FromError to avoid allocation of OK status.
	if err == nil {
		return codes.OK
	}
	if se, ok := err.(interface {
		GRPCStatus() *spb.Status
	}); ok {
		return codes.Code(se.GRPCStatus().Code)
	}
	return codes.Unknown
}

// FromContextError converts a context error into a Status.  It returns a
// Status with codes.OK if err is nil, or a Status with codes.Unknown if err is
// non-nil and not a context error.
func FromContextError(err error) *Status {
	switch err {
	case nil:
		return nil
	case context.DeadlineExceeded:
		return New(codes.DeadlineExceeded, err.Error())
	case context.Canceled:
		return New(codes.Canceled, err.Error())
	default:
		return New(codes.Unknown, err.Error())
	}
}
