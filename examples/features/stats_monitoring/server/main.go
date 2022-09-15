// Binary server is an example server.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/stats"

	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var port = flag.Int("port", 50051, "the port to serve on")

func init() {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(os.Stdout, os.Stdout, os.Stderr))
}

// *statsHandler implements [stats.Handler](https://pkg.go.dev/google.golang.org/grpc/stats#Handler) interface
type statsHandler struct{}

// TagRPC can attach some information to the given context.
// The context used for the rest lifetime of the RPC will be derived from the returned context.
func (st *statsHandler) TagRPC(ctx context.Context, stat *stats.RPCTagInfo) context.Context {
	grpclog.Infof("[%s] [TagRPC] RPC request received for: %s", ctx.Value("remoteAddr"), stat.FullMethodName)
	return ctx
}

// Note: All stat fields are read-only
func (st *statsHandler) HandleRPC(ctx context.Context, stat stats.RPCStats) {
	prompt := fmt.Sprintf("[%s] [HandleRPC] [%T]", ctx.Value("remoteAddr"), stat)

	switch stat := stat.(type) {
	case *stats.InHeader:
		grpclog.Infof("%s headers (%d bytes on wire): %v", prompt, stat.WireLength, stat.Header)

	case *stats.Begin:
		grpclog.Infof("%s request processing started at: %s", prompt, stat.BeginTime.Format(time.StampMicro))

	case *stats.InPayload:
		grpclog.Infof("%s payload (%d bytes on wire): %s", prompt, stat.WireLength, stat.Payload.(*pb.EchoRequest))
		grpclog.Infof("%s payload received from client at: %s", prompt, stat.RecvTime.Format(time.StampMicro))

	case *stats.OutHeader:
		grpclog.Infof("%s headers: %v", prompt, stat.Header)

	case *stats.OutPayload:
		grpclog.Infof("%s payload (%d bytes on wire): %s", prompt, stat.WireLength, stat.Payload.(*pb.EchoResponse))
		grpclog.Infof("%s payload sent to client at: %s", prompt, stat.SentTime.Format(time.StampMicro))

	case *stats.OutTrailer:
		grpclog.Infof("%s trailers: %v", prompt, stat.Trailer)

	case *stats.End:
		grpclog.Infof("%s response completed at: %s", prompt, stat.EndTime.Format(time.StampMicro))
		grpclog.Infof("%s request-response cycle duration (incl. 2 sec server sleep): %s", prompt, stat.EndTime.Sub(stat.BeginTime))
		if stat.Error != nil {
			grpclog.Infof("%s request-response cycle errored: %v", prompt, stat.Error)
		}
	}
}

// TagConn can attach some information to the given context.
// The context used in HandleConn for this connection will be derived from the context returned.
// In gRPC server:
// The context used in HandleRPC for RPCs on this connection will be derived from the context returned.
func (st *statsHandler) TagConn(ctx context.Context, stat *stats.ConnTagInfo) context.Context {
	grpclog.Infof("[TagConn] %s --> %s", stat.RemoteAddr, stat.LocalAddr)
	return context.WithValue(ctx, "remoteAddr", stat.RemoteAddr)
}

func (st *statsHandler) HandleConn(ctx context.Context, stat stats.ConnStats) {
	// NOP
}

type server struct {
	pb.UnimplementedEchoServer
}

func (s *server) UnaryEcho(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	time.Sleep(2 * time.Second)
	return &pb.EchoResponse{Message: req.Message}, nil
}

func main() {
	flag.Parse()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		grpclog.Fatalf("failed to listen: %v", err)
	}
	grpclog.Infof("server listening at %v\n", lis.Addr())

	s := grpc.NewServer(grpc.StatsHandler(&statsHandler{}))
	pb.RegisterEchoServer(s, &server{})
	grpclog.Fatalf("failed to serve: %v", s.Serve(lis))
}
