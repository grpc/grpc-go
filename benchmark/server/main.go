package main

import (
	"flag"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"google.golang.org/grpc/benchmark"
	"google.golang.org/grpc/grpclog"
)

var (
	duration = flag.Int("duration", math.MaxInt32, "The duration in seconds to run the benchmark server")
)

func main() {
	flag.Parse()
	go func() {
		lis, err := net.Listen("tcp", ":0")
		if err != nil {
			grpclog.Err(err).Fatal("Failed to listen")
		}
		grpclog.With("addr", lis.Addr()).Print("Server profiling")
		if err := http.Serve(lis, nil); err != nil {
			grpclog.Err(err).Fatal("Failed to serve")
		}
	}()
	addr, stopper := benchmark.StartServer(":0") // listen on all interfaces
	grpclog.With("addr", addr).Print("Server benchmark")
	<-time.After(time.Duration(*duration) * time.Second)
	stopper()
}
