/*
 *
 * Copyright 2022 gRPC authors.
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

// Package statshandler is an example pkg to illustrate the use of the stats handler.
package statshandler

import (
	"context"
	"log"
	"net"
	"path/filepath"

	"google.golang.org/grpc/stats"
)

// Handler implements [stats.Handler](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface.
type Handler struct{}

type connStatCtxKey struct{}

// TagConn can attach some information to the given context.
// The context used in HandleConn for this connection will be derived from the context returned.
// In the gRPC client:
// The context used in HandleRPC for RPCs on this connection will be the user's context and NOT derived from the context returned here.
// In the gRPC server:
// The context used in HandleRPC for RPCs on this connection will be derived from the context returned here.
func (st *Handler) TagConn(ctx context.Context, stat *stats.ConnTagInfo) context.Context {
	log.Printf("[TagConn] [%T]: %+[1]v", stat)
	return context.WithValue(ctx, connStatCtxKey{}, stat)
}

// HandleConn processes the Conn stats.
func (st *Handler) HandleConn(ctx context.Context, stat stats.ConnStats) {
	var rAddr net.Addr
	if s, ok := ctx.Value(connStatCtxKey{}).(*stats.ConnTagInfo); ok {
		rAddr = s.RemoteAddr
	}

	if stat.IsClient() {
		log.Printf("[server addr: %s] [HandleConn] [%T]: %+[2]v", rAddr, stat)
	} else {
		log.Printf("[client addr: %s] [HandleConn] [%T]: %+[2]v", rAddr, stat)
	}
}

type rpcStatCtxKey struct{}

// TagRPC can attach some information to the given context.
// The context used for the rest lifetime of the RPC will be derived from the returned context.
func (st *Handler) TagRPC(ctx context.Context, stat *stats.RPCTagInfo) context.Context {
	log.Printf("[TagRPC] [%T]: %+[1]v", stat)
	return context.WithValue(ctx, rpcStatCtxKey{}, stat)
}

// HandleRPC processes the RPC stats. Note: All stat fields are read-only.
func (st *Handler) HandleRPC(ctx context.Context, stat stats.RPCStats) {
	var sMethod string
	if s, ok := ctx.Value(rpcStatCtxKey{}).(*stats.RPCTagInfo); ok {
		sMethod = filepath.Base(s.FullMethodName)
	}

	var cAddr net.Addr
	// for gRPC clients, key connStatCtxKey{} will not be present in HandleRPC's context.
	if s, ok := ctx.Value(connStatCtxKey{}).(*stats.ConnTagInfo); ok {
		cAddr = s.RemoteAddr
	}

	if stat.IsClient() {
		log.Printf("[server method: %s] [HandleRPC] [%T]: %+[2]v", sMethod, stat)
	} else {
		log.Printf("[client addr: %s] [HandleRPC] [%T]: %+[2]v", cAddr, stat)
	}
}

// New returns a new implementation of [stats.Handler](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface.
func New() *Handler {
	return &Handler{}
}
