// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")

// *statsHandler implements [stats.Handler](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface
type statsHandler struct{}

// TagRPC can attach some information to the given context.
// The context used for the rest lifetime of the RPC will be derived from the returned context.
func (st *statsHandler) TagRPC(ctx context.Context, stat *stats.RPCTagInfo) context.Context {
	log.Printf("[TagRPC] RPC request send to: %s", stat.FullMethodName)
	return ctx
}

// Note: All stat fields are read-only
func (st *statsHandler) HandleRPC(ctx context.Context, stat stats.RPCStats) {
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

// TagConn can attach some information to the given context.
// The context used in HandleConn for this connection will be derived from the context returned.
// In gRPC client:
// The context used in HandleRPC for RPCs on this connection will NOT be derived from the context returned.
func (st *statsHandler) TagConn(ctx context.Context, stat *stats.ConnTagInfo) context.Context {
	log.Printf("[TagConn] %s --> %s", stat.LocalAddr, stat.RemoteAddr)
	return ctx
}

func (st *statsHandler) HandleConn(ctx context.Context, stat stats.ConnStats) {
	// NOP
}

func main() {
	flag.Parse()
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(&statsHandler{}),
	}
	conn, err := grpc.Dial(*addr, opts...)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewEchoClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "stats handler demo"})
	if err != nil {
		log.Fatalf("unexpected error from UnaryEcho: %v", err)
	}
	log.Println("RPC response:", resp.Message)

}
