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
	"io"
	"reflect"
	"testing"

	"github.com/google/arcwire-go/codes/codes"
	"github.com/google/arcwire-go/transport/transport"
	"code.google.com/p/goprotobuf/proto"
	"golang.org/x/net/context"
)

func TestSimpleParsing(t *testing.T) {
	for _, test := range []struct {
		// input
		p []byte
		// outputs
		err error
		b   []byte
		pt  payloadFormat
	}{
		{nil, io.EOF, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 0}, io.EOF, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 1, 'a'}, nil, []byte{'a'}, compressionNone},
		{[]byte{1, 0}, io.ErrUnexpectedEOF, nil, compressionNone},
		{[]byte{0, 0, 0, 0, 10, 'a'}, io.ErrUnexpectedEOF, nil, compressionNone},
	} {
		buf := bytes.NewReader(test.p)
		parser := &parser{buf}
		pt, b, err := parser.recvMsg()
		if err != test.err || !bytes.Equal(b, test.b) || pt != test.pt {
			t.Fatalf("parser{%v}.recvMsg() = %v, %v, %v\nwant %v, %v, %v", test.p, pt, b, err, test.pt, test.b, test.err)
		}
	}
}

func TestMultipleParsing(t *testing.T) {
	// Set a byte stream consists of 3 messages with their headers.
	p := []byte{0, 0, 0, 0, 1, 'a', 0, 0, 0, 0, 2, 'b', 'c', 0, 0, 0, 0, 1, 'd'}
	b := bytes.NewReader(p)
	parser := &parser{b}

	wantRecvs := []struct {
		pt   payloadFormat
		data []byte
	}{
		{compressionNone, []byte("a")},
		{compressionNone, []byte("bc")},
		{compressionNone, []byte("d")},
	}
	for i, want := range wantRecvs {
		pt, data, err := parser.recvMsg()
		if err != nil || pt != want.pt || !reflect.DeepEqual(data, want.data) {
			t.Fatalf("after %d calls, parser{%v}.recvMsg() = %v, %v, %v\nwant %v, %v, <nil>",
				i, p, pt, data, err, want.pt, want.data)
		}
	}

	pt, data, err := parser.recvMsg()
	if err != io.EOF {
		t.Fatalf("after %d recvMsgs calls, parser{%v}.recvMsg() = %v, %v, %v\nwant _, _, %v",
			len(wantRecvs), p, pt, data, err, io.EOF)
	}
}

func TestEncode(t *testing.T) {
	for _, test := range []struct {
		// input
		msg proto.Message
		pt  payloadFormat
		// outputs
		b   []byte
		err error
	}{
		{nil, compressionNone, []byte{0, 0, 0, 0, 0}, nil},
	} {
		b, err := encode(test.msg, test.pt)
		if err != test.err || !bytes.Equal(b, test.b) {
			t.Fatalf("encode(_, %d) = %v, %v\nwant %v, %v", test.pt, b, err, test.b, test.err)
		}
	}
}

func TestToRPCErr(t *testing.T) {
	for _, test := range []struct {
		// input
		errIn error
		// outputs
		errOut error
	}{
		{transport.StreamErrorf(codes.Unknown, ""), Errorf(codes.Unknown, "")},
		{transport.ErrConnClosing, Errorf(codes.Internal, transport.ErrConnClosing.Desc)},
	} {
		err := toRPCErr(test.errIn)
		if err != test.errOut {
			t.Fatalf("toRPCErr{%v} = %v \nwant %v", test.errIn, err, test.errOut)
		}
	}
}

func TestContextErr(t *testing.T) {
	for _, test := range []struct {
		// input
		errIn error
		// outputs
		errOut transport.StreamError
	}{
		{context.DeadlineExceeded, transport.StreamErrorf(codes.DeadlineExceeded, "%v", context.DeadlineExceeded)},
		{context.Canceled, transport.StreamErrorf(codes.Canceled, "%v", context.Canceled)},
	} {
		err := transport.ContextErr(test.errIn)
		if err != test.errOut {
			t.Fatalf("ContextErr{%v} = %v \nwant %v", test.errIn, err, test.errOut)
		}
	}
}
