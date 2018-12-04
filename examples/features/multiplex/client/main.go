/*
 *
 * Copyright 2018 gRPC authors.
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

package main

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	rgpb "google.golang.org/grpc/examples/route_guide/routeguide"
)

const (
	address = "localhost:50051"
)

// callSayHello calls SayHello on c with the given name, and prints the
// response.
func callSayHello(c hwpb.GreeterClient, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &hwpb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	log.Printf("Greeting: %s", r.Message)
}

// callPrintFeature gets the feature for the given point.
func callPrintFeature(client rgpb.RouteGuideClient, point *rgpb.Point) {
	log.Printf("Getting feature for point (%d, %d)", point.Latitude, point.Longitude)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	feature, err := client.GetFeature(ctx, point)
	if err != nil {
		log.Fatalf("%v.GetFeatures(_) = _, %v: ", client, err)
	}
	log.Println(feature)
}

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	log.Println("--- calling helloworld.Greeter/SayHello ---")
	// Make a greeter client and send an RPC.
	hwc := hwpb.NewGreeterClient(conn)
	callSayHello(hwc, "multiplex")

	log.Println()
	log.Println("--- calling routeguide.RouteGuide/GetFeature ---")
	// Make a routeguild client with the same ClientConn.
	rgc := rgpb.NewRouteGuideClient(conn)
	callPrintFeature(rgc, &rgpb.Point{Latitude: 314, Longitude: 271})
}
