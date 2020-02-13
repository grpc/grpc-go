package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

var (
	addr   = flag.String("addr", "localhost:50051", "the address to connect to")
	system = "" // empty string represents the health of the system
)

func callCheck(client healthpb.HealthClient) {
	response, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{
		Service: system,
	})

	if err != nil {
		log.Fatalf("client.Check(_) = _, %v: ", err)
	}

	fmt.Println("Check: ", response.Status)
}

func callWatch(client healthpb.HealthClient) {
	watch, err := client.Watch(context.Background(), &healthpb.HealthCheckRequest{
		Service: system,
	})

	if err != nil {
		log.Fatalf("client.Watch(_) = _, %v: ", err)
	}
	watch.CloseSend()

	for {
		response, err := watch.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatalf("watch.Recv() = _, %v: ", err)
		}

		fmt.Println("Watch: ", response.Status)
		time.Sleep(time.Second)
	}
}

func main() {
	flag.Parse()

	conn, err := grpc.Dial(*addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect %v", err)
	}
	defer conn.Close()

	healthClient := healthpb.NewHealthClient(conn)
	callCheck(healthClient)
	callWatch(healthClient)
}
