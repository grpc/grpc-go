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

package grpc

import (
	"errors"
	"io"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/transport"
)

type streamHandler func(srv interface{}, stream ServerStream) error

// StreamDesc represents a streaming RPC service's method specification.
type StreamDesc struct {
	StreamName string
	Handler    streamHandler

	// At least one of these is true.
	ServerStreams bool
	ClientStreams bool
}

// Stream defines the common interface a client or server stream has to satisfy.
type Stream interface {
	// Context returns the context for this stream.
	Context() context.Context
	// SendMessage blocks until it sends m, the stream is done or the stream
	// breaks.
	// On error, it aborts the stream and returns an RPC status on client
	// side. On server side, it simply returns the error to the caller.
	// SendMessage is called by generated code.
	SendMessage(f Formatter) error
	// RecvMessage blocks until it receives a proto message or the stream is
	// done. On client side, it returns io.EOF when the stream is done. On
	// any other error, it aborts the streama nd returns an RPC status. On
	// server side, it simply returns the error to the caller.
	RecvMessage(f Formatter) error
}

// ClientStream defines the interface a client stream has to satify.
type ClientStream interface {
	// Header returns the header metedata received from the server if there
	// is any. It blocks if the metadata is not ready to read.
	Header() (metadata.MD, error)
	// Trailer returns the trailer metadata from the server. It must be called
	// after stream.Recv() returns non-nil error (including io.EOF) for
	// bi-directional streaming and server streaming or stream.CloseAndRecv()
	// returns for client streaming in order to receive trailer metadata if
	// present. Otherwise, it could returns an empty MD even though trailer
	// is present.
	Trailer() metadata.MD
	// CloseSend closes the send direction of the stream. It closes the stream
	// when non-nil error is met.
	CloseSend() error
	Stream
}

// NewClientStream creates a new Stream for the client side. This is called
// by generated code.
func NewClientStream(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, opts ...CallOption) (ClientStream, error) {
	// TODO(zhaoq): CallOption is omitted. Add support when it is needed.
	host, _, err := net.SplitHostPort(cc.target)
	if err != nil {
		return nil, toRPCErr(err)
	}
	callHdr := &transport.CallHdr{
		Host:   host,
		Method: method,
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
		t:    t,
		s:    s,
		p:    &parser{s: s},
		desc: desc,
	}, nil
}

// clientStream implements a client side Stream.
type clientStream struct {
	t    transport.ClientTransport
	s    *transport.Stream
	p    *parser
	desc *StreamDesc
}

func (cs *clientStream) Context() context.Context {
	return cs.s.Context()
}

func (cs *clientStream) Header() (metadata.MD, error) {
	m, err := cs.s.Header()
	if err != nil {
		if _, ok := err.(transport.ConnectionError); !ok {
			cs.t.CloseStream(cs.s, err)
		}
	}
	return m, err
}

func (cs *clientStream) Trailer() metadata.MD {
	return cs.s.Trailer()
}

func (cs *clientStream) SendMessage(m Formatter) (err error) {
	defer func() {
		if err == nil || err == io.EOF {
			return
		}
		if _, ok := err.(transport.ConnectionError); !ok {
			cs.t.CloseStream(cs.s, err)
		}
		err = toRPCErr(err)
	}()
	out, err := encode(m, compressionNone)
	if err != nil {
		return transport.StreamErrorf(codes.Internal, "grpc: %v", err)
	}
	return cs.t.Write(cs.s, out, &transport.Options{Last: false})
}

func (cs *clientStream) RecvMessage(m Formatter) (err error) {
	err = recvMsg(cs.p, m)
	if err == nil {
		if !cs.desc.ClientStreams || cs.desc.ServerStreams {
			return
		}
		// Special handling for client streaming rpc.
		err = recvMsg(cs.p, m)
		cs.t.CloseStream(cs.s, err)
		if err == nil {
			return toRPCErr(errors.New("grpc: client streaming protocol violation: get <nil>, want <EOF>"))
		}
		if err == io.EOF {
			if cs.s.StatusCode() == codes.OK {
				return nil
			}
			return Errorf(cs.s.StatusCode(), cs.s.StatusDesc())
		}
		return toRPCErr(err)
	}
	if _, ok := err.(transport.ConnectionError); !ok {
		cs.t.CloseStream(cs.s, err)
	}
	if err == io.EOF {
		if cs.s.StatusCode() == codes.OK {
			// Returns io.EOF to indicate the end of the stream.
			return
		}
		return Errorf(cs.s.StatusCode(), cs.s.StatusDesc())
	}
	return toRPCErr(err)
}

func (cs *clientStream) CloseSend() (err error) {
	err = cs.t.Write(cs.s, nil, &transport.Options{Last: true})
	if err == nil || err == io.EOF {
		return
	}
	if _, ok := err.(transport.ConnectionError); !ok {
		cs.t.CloseStream(cs.s, err)
	}
	err = toRPCErr(err)
	return
}

// ServerStream defines the interface a server stream has to satisfy.
type ServerStream interface {
	// SendHeader sends the header metadata. It should not be called
	// after Send. It fails if called multiple times or if
	// called after Send.
	SendHeader(metadata.MD) error
	// SetTrailer sets the trailer metadata which will be sent with the
	// RPC status.
	SetTrailer(metadata.MD)
	Stream
}

// serverStream implements a server side Stream.
type serverStream struct {
	t          transport.ServerTransport
	s          *transport.Stream
	p          *parser
	statusCode codes.Code
	statusDesc string
}

func (ss *serverStream) Context() context.Context {
	return ss.s.Context()
}

func (ss *serverStream) SendHeader(md metadata.MD) error {
	return ss.t.WriteHeader(ss.s, md)
}

func (ss *serverStream) SetTrailer(md metadata.MD) {
	if md.Len() == 0 {
		return
	}
	ss.s.SetTrailer(md)
	return
}

func (ss *serverStream) SendMessage(m Formatter) error {
	out, err := encode(m, compressionNone)
	if err != nil {
		err = transport.StreamErrorf(codes.Internal, "grpc: %v", err)
		return err
	}
	return ss.t.Write(ss.s, out, &transport.Options{Last: false})
}

func (ss *serverStream) RecvMessage(m Formatter) error {
	return recvMsg(ss.p, m)
}
