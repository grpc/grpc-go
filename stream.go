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
	"io"

	"github.com/google/arcwire-go/codes/codes"
	"github.com/google/arcwire-go/transport/transport"
	"code.google.com/p/goprotobuf/proto"
	"golang.org/x/net/context"
)

// Stream defines the common interface a client or server stream has to satisfy.
type Stream interface {
	// Context returns the context for this stream.
	Context() context.Context
	// SendProto blocks until it sends a proto message out or some
	// error happens.
	SendProto(proto.Message) error
	// RecvProto blocks until either a proto message is received or some
	// error happens.
	RecvProto(proto.Message) error
}

// ClientStream defines the interface a client stream has to satify.
type ClientStream interface {
	// CloseSend closes the send direction of the stream.
	CloseSend() error
	Stream
}

// NewClientStream creates a new Stream for the client side. This is called
// by generated code.
func NewClientStream(ctx context.Context, cc *ClientConn, method string, opts ...CallOption) (ClientStream, error) {
	var copts CallOptions
	for _, o := range opts {
		o.set(&copts)
	}
	callHdr := &transport.CallHdr{
		Host:   cc.target,
		Method: method,
		Auth:   copts.Auth,
	}
	t, _, err := cc.wait(ctx, 0)
	if err != nil {
		return nil, toRPCErr(err)
	}
	s, err := t.NewStream(ctx, callHdr)
	if err != nil {
		return nil, toRPCErr(err)
	}
	return &clientStream{
		t: t,
		s: s,
		p: &parser{s: s},
	}, nil
}

// clientStream implements a client side Stream.
type clientStream struct {
	t transport.ClientTransport
	s *transport.Stream
	p *parser
}

// Context returns the clientStream's associated context.
func (cs *clientStream) Context() context.Context {
	return cs.s.Context()
}

// SendProto blocks until m is sent out or an error happens. It closes the
// stream when a non-nil error is met. This is called by generated code.
func (cs *clientStream) SendProto(m proto.Message) (err error) {
	defer func() {
		if err != nil && err != io.EOF {
			if _, ok := err.(transport.StreamError); ok {
				cs.t.CloseStream(cs.s, err)
			}
			err = toRPCErr(err)
		}
	}()
	out, err := encode(m, compressionNone)
	if err != nil {
		return transport.StreamErrorf(codes.Internal, "%v", err)
	}
	return cs.t.Write(cs.s, out, &transport.Options{Last: false})
}

// RecvProto blocks until it receives a proto message or an error happens.
// When an non-nil error (including EOF which indicates the success of an
// RPC) is met, it also closes the stream and returns the RPC status to
// the caller. This is called by generated code.
func (cs *clientStream) RecvProto(m proto.Message) (err error) {
	err = recvProto(cs.p, m)
	if err == nil {
		return
	}
	if _, ok := err.(transport.StreamError); ok {
		cs.t.CloseStream(cs.s, err)
	}
	if err != io.EOF {
		return toRPCErr(err)
	}
	if cs.s.StatusCode() == codes.OK {
		return
	}
	return Errorf(cs.s.StatusCode(), cs.s.StatusDesc())
}

// CloseSend closes the send direction of the stream. It closes the stream
// when non-nil error is met.
func (cs *clientStream) CloseSend() (err error) {
	defer func() {
		if err != nil && err != io.EOF {
			if _, ok := err.(transport.StreamError); ok {
				cs.t.CloseStream(cs.s, err)
			}
			err = toRPCErr(err)
		}
	}()
	out, err := encode(nil, compressionNone)
	if err != nil {
		return transport.StreamErrorf(codes.Internal, "%v", err)
	}
	return cs.t.Write(cs.s, out, &transport.Options{Last: true})
}

// serverStream implements a server side Stream.
type serverStream struct {
	t          transport.ServerTransport
	s          *transport.Stream
	p          *parser
	statusCode codes.Code
	statusDesc string
}

// Context returns the associated context so that server applications can
// manipulate it.
func (ss *serverStream) Context() context.Context {
	return ss.s.Context()
}

// SendProto blocks until m is sent out or an error is met. This is called by
// generated code.
func (ss *serverStream) SendProto(m proto.Message) error {
	out, err := encode(m, compressionNone)
	if err != nil {
		err = transport.StreamErrorf(codes.Internal, "%v", err)
		return err
	}
	return ss.t.Write(ss.s, out, &transport.Options{Last: false})
}

// RecvProto blocks until it receives a message or an error is met. This is
// called by generated code.
func (ss *serverStream) RecvProto(m proto.Message) error {
	return recvProto(ss.p, m)
}
