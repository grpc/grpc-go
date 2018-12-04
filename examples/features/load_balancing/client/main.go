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
	"fmt"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/resolver"
)

const splitScheme = "split"

var addrs = []string{"localhost:50051", "localhost:50052"}

type splitResolverBuilder struct{}

func (*splitResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOption) (resolver.Resolver, error) {
	r := &splitResolver{target: target, cc: cc}
	r.start()
	return r, nil
}
func (*splitResolverBuilder) Scheme() string { return splitScheme }

type splitResolver struct {
	target resolver.Target
	cc     resolver.ClientConn
}

func (r *splitResolver) start() {
	splitted := strings.Split(r.target.Endpoint, ",")
	addrs := make([]resolver.Address, len(splitted), len(splitted))
	for i, s := range splitted {
		addrs[i] = resolver.Address{Addr: s}
	}
	r.cc.NewAddress(addrs)
}
func (*splitResolver) ResolveNow(o resolver.ResolveNowOption) {}
func (*splitResolver) Close()                                 {}

func init() { resolver.Register(&splitResolverBuilder{}) }

///////////////////////////////////////////////////////////////////////

// callSayHello calls SayHello on c with the given name, and prints the
// response.
func callSayHello(c hwpb.GreeterClient, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r, err := c.SayHello(ctx, &hwpb.HelloRequest{Name: name})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	fmt.Println(r.Message)
}

func makeRPCs(cc *grpc.ClientConn, n int) {
	hwc := hwpb.NewGreeterClient(cc)
	for i := 0; i < n; i++ {
		callSayHello(hwc, "lb")
	}
}

func main() {
	pickfirstConn, err := grpc.Dial(
		fmt.Sprintf("%s:///%s", splitScheme, strings.Join(addrs, ",")),
		// grpc.WithBalancerName("pick_first"), // "pick_first" is the default, so this DialOption is not necessary.
		grpc.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer pickfirstConn.Close()

	fmt.Println("--- calling helloworld.Greeter/SayHello with pick_first ---")
	makeRPCs(pickfirstConn, 10)

	fmt.Println()

	// Make another ClientConn with round_robin policy.
	roundrobinConn, err := grpc.Dial(
		fmt.Sprintf("%s:///%s", splitScheme, strings.Join(addrs, ",")),
		grpc.WithBalancerName("round_robin"), // This sets the initial balancing policy.
		grpc.WithInsecure(),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer roundrobinConn.Close()

	fmt.Println("--- calling helloworld.Greeter/SayHello with round_robin ---")
	makeRPCs(roundrobinConn, 10)
}
