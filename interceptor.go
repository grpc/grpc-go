/*
 *
 * Copyright 2016, Google Inc.
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
	"golang.org/x/net/context"
)

// UnaryServerInfo consists of various information about a unary RPC on
// server side. All per-rpc information may be mutated by the interceptor.
type UnaryServerInfo struct {
	// Server is the service implementation the user provides. This is read-only.
	Server interface{}
	// FullMethod is the full RPC method string, i.e., /package.service/method.
	FullMethod string
}

// UnaryHandler defines the handler invoked by UnaryServerInterceptor to complete the normal
// execution of a unary RPC.
type UnaryHandler func(ctx context.Context, req interface{}) (interface{}, error)

// UnaryServerInterceptor provides a hook to intercept the execution of a unary RPC on the server. info
// contains all the information of this RPC the interceptor can operate on. And handler is the wrapper
// of the service method implementation. It is the responsibility of the interceptor to invoke handler
// to complete the RPC.
type UnaryServerInterceptor func(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (resp interface{}, err error)

// StreamServerInfo consists of various information about a streaming RPC on
// server side. All per-rpc information may be mutated by the interceptor.
type StreamServerInfo struct {
	// FullMethod is the full RPC method string, i.e., /package.service/method.
	FullMethod string
	// IsClientStream indicates whether the RPC is a client streaming RPC.
	IsClientStream bool
	// IsServerStream indicates whether the RPC is a server streaming RPC.
	IsServerStream bool
}

// StreamServerInterceptor provides a hook to intercept the execution of a streaming RPC on the server.
// info contains all the information of this RPC the interceptor can operate on. And handler is the
// service method implementation. It is the responsibility of the interceptor to invoke handler to
// complete the RPC.
type StreamServerInterceptor func(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error

type multiUnaryServerInterceptor struct {
	interceptors []UnaryServerInterceptor
}

func newMultiUnaryServerInterceptor(interceptors ...UnaryServerInterceptor) UnaryServerInterceptor {
	return (&multiUnaryServerInterceptor{interceptors}).do
}

func (m *multiUnaryServerInterceptor) do(ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
	return m.doRec(0, ctx, req, info, handler)
}

func (m *multiUnaryServerInterceptor) doRec(i int, ctx context.Context, req interface{}, info *UnaryServerInfo, handler UnaryHandler) (interface{}, error) {
	if i == len(m.interceptors) {
		return handler(ctx, req)
	}
	return m.interceptors[i](ctx, req, info, func(c context.Context, r interface{}) (interface{}, error) {
		return m.doRec(i+1, c, r, info, handler)
	})
}

type multiStreamServerInterceptor struct {
	interceptors []StreamServerInterceptor
}

func newMultiStreamServerInterceptor(interceptors ...StreamServerInterceptor) StreamServerInterceptor {
	return (&multiStreamServerInterceptor{interceptors}).do
}

func (m *multiStreamServerInterceptor) do(srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
	return m.doRec(0, srv, ss, info, handler)
}

func (m *multiStreamServerInterceptor) doRec(i int, srv interface{}, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error {
	if i == len(m.interceptors) {
		return handler(srv, ss)
	}
	return m.interceptors[i](srv, ss, info, func(s interface{}, s2 ServerStream) error {
		return m.doRec(i+1, s, s2, info, handler)
	})
}
