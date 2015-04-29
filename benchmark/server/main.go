package main

import (
	"flag"
	"fmt"
	"math"
	"time"

	"google.golang.org/grpc/benchmark"
)

var (
	duration = flag.Int("duration", math.MaxInt32, "The duration in seconds to run the benchmark server")
)

func main() {
	flag.Parse()
	addr, stopper := benchmark.StartServer()
	fmt.Println("Server Address: ", addr)
	<-time.After(time.Duration(*duration) * time.Second)
	stopper()
}
