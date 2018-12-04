package client

import (
	"context"
	"log"
	"time"

	pb "google.golang.org/grpc/examples/helloworld/helloworld"
)

// CallSayHello calls SayHello on c with the given name, and prints the
// response.
func CallSayHello(c pb.GreeterClient, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &pb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.Message)
}
