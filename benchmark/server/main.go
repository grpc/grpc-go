package main

import (
	"flag"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"time"

	"google.golang.org/grpc/benchmark"
	"google.golang.org/grpc/logs"
)

var (
	duration = flag.Int("duration", math.MaxInt32, "The duration in seconds to run the benchmark server")
	logger   = logs.DefaultLogger
)

func main() {
	flag.Parse()
	go func() {
		lis, err := net.Listen("tcp", ":0")
		if err != nil {
			logger.Fatalf("Failed to listen: %v", err)
		}
		logger.Println("Server profiling address: ", lis.Addr().String())
		if err := http.Serve(lis, nil); err != nil {
			logger.Fatalf("Failed to serve: %v", err)
		}
	}()
	addr, stopper := benchmark.StartServer(logger)
	logger.Println("Server Address: ", addr)
	<-time.After(time.Duration(*duration) * time.Second)
	stopper()
}
