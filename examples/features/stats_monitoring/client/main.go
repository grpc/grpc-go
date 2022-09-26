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

// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")

// *statsHandler implements [stats.Handler](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface
type statsHandler struct{}

type rpcStatCtxKey struct{}

// TagRPC can attach some information to the given context.
// The context used for the rest lifetime of the RPC will be derived from the returned context.
func (st *statsHandler) TagRPC(ctx context.Context, stat *stats.RPCTagInfo) context.Context {
	log.Printf("[%s] [TagRPC] [%T]: %+[2]v", filepath.Base(stat.FullMethodName), stat)
	return context.WithValue(ctx, rpcStatCtxKey{}, stat)
}

// Note: All stat fields are read-only
func (st *statsHandler) HandleRPC(ctx context.Context, stat stats.RPCStats) {
	var sMethod string
	if s, ok := ctx.Value(rpcStatCtxKey{}).(*stats.RPCTagInfo); ok {
		sMethod = filepath.Base(s.FullMethodName)
	}

	// following string to be used in case the switch statement is removed
	_ = fmt.Sprintf("[%s] [HandleRPC] [%T]: %+[2]v", sMethod, stat)
	// log.Printf("[%s] [HandleRPC] [%T]: %+[2]v", sMethod, stat)

	switch stat := stat.(type) {
	case *stats.Begin:
		log.Printf("[HandleRPC] [%T] Request sending process started at: %s", stat, stat.BeginTime.Format(time.StampMicro))

	case *stats.OutHeader:
		log.Printf("[HandleRPC] [%T] headers: %v", stat, stat.Header)

	case *stats.OutPayload:
		log.Printf("[HandleRPC] [%T] payload (%d bytes on wire): %s", stat, stat.WireLength, stat.Payload.(*pb.EchoRequest))
		log.Printf("[HandleRPC] [%T] Request sending process completed at: %s", stat, stat.SentTime.Format(time.StampMicro))

	case *stats.InHeader:
		log.Printf("[HandleRPC] [%T] headers (%d bytes on wire): %v", stat, stat.WireLength, stat.Header)

	case *stats.InTrailer:
		log.Printf("[HandleRPC] [%T] trailers (%d bytes on wire): %v", stat, stat.WireLength, stat.Trailer)

	case *stats.InPayload:
		log.Printf("[HandleRPC] [%T] payload (%d bytes on wire): %s", stat, stat.WireLength, stat.Payload.(*pb.EchoResponse))
		log.Printf("[HandleRPC] [%T] payload received at: %s", stat, stat.RecvTime.Format(time.StampMicro))

	case *stats.End:
		log.Printf("[HandleRPC] [%T] response completed at: %s", stat, stat.EndTime.Format(time.StampMicro))
		log.Printf("[HandleRPC] [%T] request-response cycle duration (incl. 2 sec server sleep): %s", stat, stat.EndTime.Sub(stat.BeginTime))
		if stat.Error != nil {
			log.Printf("[HandleRPC] [%T] request-response cycle errored: %v", stat, stat.Error)
		}
	}
}

type connStatCtxKey struct{}

// TagConn can attach some information to the given context.
// The context used in HandleConn for this connection will be derived from the context returned.
// In gRPC client:
// The context used in HandleRPC for RPCs on this connection will NOT be derived from the context returned.
func (st *statsHandler) TagConn(ctx context.Context, stat *stats.ConnTagInfo) context.Context {
	log.Printf("[TagConn] %s --> %s", stat.LocalAddr, stat.RemoteAddr)
	return context.WithValue(ctx, connStatCtxKey{}, stat)
}

func (st *statsHandler) HandleConn(ctx context.Context, stat stats.ConnStats) {
	var sAddr net.Addr
	if s, ok := ctx.Value(connStatCtxKey{}).(*stats.ConnTagInfo); ok {
		sAddr = s.RemoteAddr
	}

	// following string to be used in case the switch statement is removed
	_ = fmt.Sprintf("[%s] [HandleConn] [%T]: %+[2]v", sAddr, stat)
	// log.Printf("[%s] [HandleConn] [%T]: %+[2]v", sAddr, stat)
}

func main() {
	flag.Parse()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(&statsHandler{}),
	}
	conn, err := grpc.Dial(*addr, opts...)
	if err != nil {
		log.Fatalf("failed to connect to server %q: %v", *addr, err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := pb.NewEchoClient(conn)

	resp, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "stats handler demo"})
	if err != nil {
		log.Fatalf("unexpected error from UnaryEcho: %v", err)
	}
	log.Printf("RPC response: %s", resp.Message)

}
