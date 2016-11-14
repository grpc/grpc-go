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

package stats

import (
	"net"
	"sync/atomic"

	"golang.org/x/net/context"
	"google.golang.org/grpc/grpclog"
)

// ConnContextTagInfo defines the relevant information needed by connection context tagger.
type ConnContextTagInfo struct {
	// RemoteAddr is the remote address of the corresponding connection.
	RemoteAddr net.Addr
	// LocalAddr is the local address of the corresponding connection.
	LocalAddr net.Addr
}

// RPCContextTagInfo defines the relevant information needed by RPC context tagger.
type RPCContextTagInfo struct {
	// FullMethodName is the string of gRPC method (in the format of /package.service/method).
	FullMethodName string
}

var (
	on          = new(int32)
	handler     func(context.Context, RPCStats)
	connhandler func(context.Context, ConnStats)
	connTagger  func(context.Context, *ConnContextTagInfo) context.Context
	rpcTagger   func(context.Context, *RPCContextTagInfo) context.Context
)

// Handle processes the stats using the call back function registered by user.
func Handle(ctx context.Context, s RPCStats) {
	if handler == nil {
		return
	}
	handler(ctx, s)
}

// RegisterHandler registers the user handler function for rpc stats.
// If another handler was registered before, this new handler will overwrite the old one.
// This handler function will be called to process the rpc stats.
func RegisterHandler(f func(context.Context, RPCStats)) {
	handler = f
}

// ConnHandle processes the stats using the call back function registered by user.
func ConnHandle(ctx context.Context, s ConnStats) {
	if connhandler == nil {
		return
	}
	connhandler(ctx, s)
}

// RegisterConnHandler registers the user handler function for conn stats.
// If another handler was registered before, this new handler will overwrite the old one.
// This handler function will be called to process the conn stats.
func RegisterConnHandler(f func(context.Context, ConnStats)) {
	connhandler = f
}

// TagConnCtx calls user registered connection context tagger.
func TagConnCtx(ctx context.Context, info *ConnContextTagInfo) context.Context {
	if connTagger == nil {
		return ctx
	}
	return connTagger(ctx, info)
}

// RegisterConnCtxTagger registers the user connection context tagger function.
// The connection context tagger can attach some information to the given context.
// The returned context will be used for stats handling.
// For conn stats handling, the context used in conn stats handler for this
// connection will be derived from the context returned.
// For RPC stats handling,
//  - On server side, the context used in RPC stats handler for all RPCs using this
// connection will be derived from the context returned.
//  - On client side, the context is not derived from the context returned.
func RegisterConnCtxTagger(t func(context.Context, *ConnContextTagInfo) context.Context) {
	connTagger = t
}

// TagRPCCtx calls user registered RPC context tagger.
func TagRPCCtx(ctx context.Context, info *RPCContextTagInfo) context.Context {
	if rpcTagger == nil {
		return ctx
	}
	return rpcTagger(ctx, info)
}

// RegisterRPCCtxTagger registers the user RPC context tagger function.
// The RPC context tagger can attach some information to the given context.
// The context used in stats handler for this RPC will be derived from the
// context returned.
func RegisterRPCCtxTagger(t func(context.Context, *RPCContextTagInfo) context.Context) {
	rpcTagger = t
}

// Start starts the stats collection and reporting if there is a registered stats handle.
func Start() {
	if handler == nil && connhandler == nil {
		grpclog.Println("handler and connhandler are both nil when starting stats. Stats is not started")
		return
	}
	atomic.StoreInt32(on, 1)
}

// Stop stops the stats collection and processing.
// Stop does not unregister handler.
func Stop() {
	atomic.StoreInt32(on, 0)
}

// On indicates whether stats is started.
func On() bool {
	return atomic.CompareAndSwapInt32(on, 1, 1)
}
