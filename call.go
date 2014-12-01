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

	"github.com/google/arcwire-go/auth/auth"
	"github.com/google/arcwire-go/codes/codes"
	"github.com/google/arcwire-go/transport/transport"
	"code.google.com/p/goprotobuf/proto"
	"golang.org/x/net/context"
)

// recv receives and parses an RPC response.
// On error, it returns the error and indicates whether the call should be retried.
//
// TODO(zhaoq): Check whether the received message sequence is valid.
func recv(stream *transport.Stream, reply proto.Message) error {
	p := &parser{s: stream}
	for {
		if err := recvProto(p, reply); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// CallOptions specifies the requirements of RPC.
type CallOptions struct {
	FailFast bool
	Auth     auth.Manager
}

// sendRPC writes out various information of an RPC such as Context and Message.
func sendRPC(ctx context.Context, callHdr *transport.CallHdr, t transport.ClientTransport, args proto.Message, opts *transport.Options) (_ *transport.Stream, err error) {
	stream, err := t.NewStream(ctx, callHdr)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			if _, ok := err.(transport.ConnectionError); !ok {
				t.CloseStream(stream, err)
			}
		}
	}()
	// TODO(zhaoq): Support compression.
	outBuf, err := encode(args, compressionNone)
	if err != nil {
		return nil, transport.StreamErrorf(codes.Internal, "%v", err)
	}
	err = t.Write(stream, outBuf, opts)
	if err != nil {
		return nil, err
	}
	// Sent successfully.
	return stream, nil
}

// CallOption modifies CallOptions.
type CallOption interface {
	set(*CallOptions)
}

// Invoke is called by the generated code. It sends the RPC request on the
// wire and returns after response is received.
func Invoke(ctx context.Context, method string, args, reply proto.Message, cc *ClientConn, opts ...CallOption) error {
	var copts CallOptions
	for _, o := range opts {
		o.set(&copts)
	}
	callHdr := &transport.CallHdr{
		Host:   cc.target,
		Method: method,
		Auth:   copts.Auth,
	}
	topts := &transport.Options{
		Last:  true,
		Delay: false,
	}
	ts := 0
	var lastErr error // record the error happened in the last iteration
	for {
		var (
			err    error
			t      transport.ClientTransport
			stream *transport.Stream
		)
		// TODO(zhaoq): Need a formal spec of retry strategy for non-failfast rpcs.
		if lastErr != nil && copts.FailFast {
			return lastErr
		}
		t, ts, err = cc.wait(ctx, ts)
		if err != nil {
			if lastErr != nil {
				// This was a retry; return the error from the last attempt.
				return lastErr
			}
			return err
		}
		stream, err = sendRPC(ctx, callHdr, t, args, topts)
		if err != nil {
			if _, ok := err.(transport.ConnectionError); ok {
				lastErr = err
				continue
			}
			if lastErr != nil {
				return toRPCErr(lastErr)
			}
			return toRPCErr(err)
		}
		// Receive the response
		lastErr = recv(stream, reply)
		if _, ok := lastErr.(transport.ConnectionError); ok {
			continue
		}
		t.CloseStream(stream, lastErr)
		if lastErr != nil {
			return toRPCErr(lastErr)
		}
		return Errorf(stream.StatusCode(), stream.StatusDesc())
	}
}
