/*
 *
 * Copyright 2014, Google Inc.
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

package rpc

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/google/arcwire-go/codes/codes"
	"github.com/google/arcwire-go/transport/transport"
	"code.google.com/p/goprotobuf/proto"
	"golang.org/x/net/context"
)

// The format of the payload: compressed or not?
type payloadFormat uint8

const (
	compressionNone payloadFormat = iota // no compression
	compressionFlate
	// More formats
)

// parser reads complelete gRPC messages from the underlying reader.
type parser struct {
	s io.Reader
}

// msgFixedHeader defines the header of a gRPC message (go/grpc-wirefmt).
type msgFixedHeader struct {
	T      payloadFormat
	Length uint32
}

// recvMsg is to read a complete gRPC message from the stream. It is blocking if
// the message has not been complete yet. It returns the message and its type,
// EOF is returned with nil msg and 0 pf if the entire stream is done. Other
// non-nil error is returned if something is wrong on reading.
func (p *parser) recvMsg() (pf payloadFormat, msg []byte, err error) {
	var hdr msgFixedHeader
	if err := binary.Read(p.s, binary.BigEndian, &hdr); err != nil {
		return 0, nil, err
	}
	if hdr.Length == 0 {
		return hdr.T, nil, io.EOF
	}
	msg = make([]byte, int(hdr.Length))
	if _, err := io.ReadFull(p.s, msg); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return 0, nil, err
	}
	return hdr.T, msg, nil
}

// encode serializes msg and prepends the message header. If msg is nil, it
// generates the message header of 0 message length.
func encode(msg proto.Message, pf payloadFormat) ([]byte, error) {
	var buf bytes.Buffer
	// Write message fixed header.
	if err := buf.WriteByte(uint8(pf)); err != nil {
		return nil, err
	}
	var b []byte
	var length uint32
	if msg != nil {
		var err error
		// TODO(zhaoq): optimize to reduce memory alloc and copying.
		b, err = proto.Marshal(msg)
		if err != nil {
			return nil, err
		}
		length = uint32(len(b))
	}
	if err := binary.Write(&buf, binary.BigEndian, length); err != nil {
		return nil, err
	}
	if _, err := buf.Write(b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func recvProto(p *parser, m proto.Message) error {
	pf, d, err := p.recvMsg()
	if err != nil {
		return err
	}
	switch pf {
	case compressionNone:
		if err := proto.Unmarshal(d, m); err != nil {
			return Errorf(codes.Internal, "%v", err)
		}
	default:
		return Errorf(codes.Internal, "compression is not supported yet.")
	}
	return nil
}

// rpcError defines the status from an RPC.
type rpcError struct {
	code codes.Code
	desc string
}

func (e rpcError) Error() string {
	return fmt.Sprintf("rpc error: code = %d desc = %q", e.code, e.desc)
}

// Code returns the error code for err if it was produced by the rpc system.
// Otherwise, it returns codes.Unknown.
func Code(err error) codes.Code {
	if e, ok := err.(rpcError); ok {
		return e.code
	}
	return codes.Unknown
}

// Errorf returns an error containing an error code and a description;
// CodeOf extracts the Code.
// Errorf returns nil if c is OK.
func Errorf(c codes.Code, format string, a ...interface{}) error {
	if c == codes.OK {
		return nil
	}
	return rpcError{
		code: c,
		desc: fmt.Sprintf(format, a...),
	}
}

// toRPCErr converts a transport error into a rpcError if possible.
func toRPCErr(err error) error {
	switch e := err.(type) {
	case transport.StreamError:
		return rpcError{
			code: e.Code,
			desc: e.Desc,
		}
	case transport.ConnectionError:
		return rpcError{
			code: codes.Internal,
			desc: e.Desc,
		}
	}
	return Errorf(codes.Unknown, "failed to convert %v to rpcErr", err)
}

// convertCode converts a standard Go error into its canonical code. Note that
// this is only used to translate the error returned by the server applications.
func convertCode(err error) codes.Code {
	switch err {
	case nil:
		return codes.OK
	case io.EOF:
		return codes.OutOfRange
	case io.ErrClosedPipe, io.ErrNoProgress, io.ErrShortBuffer, io.ErrShortWrite, io.ErrUnexpectedEOF:
		return codes.FailedPrecondition
	case os.ErrInvalid:
		return codes.InvalidArgument
	case context.Canceled:
		return codes.Canceled
	case context.DeadlineExceeded:
		return codes.DeadlineExceeded
	}
	switch {
	case os.IsExist(err):
		return codes.AlreadyExists
	case os.IsNotExist(err):
		return codes.NotFound
	case os.IsPermission(err):
		return codes.PermissionDenied
	}
	return codes.Unknown
}
