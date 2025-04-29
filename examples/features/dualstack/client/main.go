/*
 *
 * Copyright 2025 gRPC authors.
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

// Binary client is a client for the dualstack example.
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	hwpb "google.golang.org/grpc/examples/helloworld/helloworld"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
)

const (
	port1 = 50051
	port2 = 50052
	port3 = 50053
)

func init() {
	resolver.Register(&exampleResolver{})
}

// exampleResolver implements both, a fake `resolver.Resolver` and
// `resolver.Builder`. This resolver sends a hard-coded list of 3 endpoints each
// with 2 addresses (one IPv4 and one IPv6) to the channel.
type exampleResolver struct{}

func (*exampleResolver) Close() {}

func (*exampleResolver) ResolveNow(resolver.ResolveNowOptions) {}

func (*exampleResolver) Build(_ resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	go func() {
		err := cc.UpdateState(resolver.State{
			Endpoints: []resolver.Endpoint{
				{Addresses: []resolver.Address{
					{Addr: fmt.Sprintf("[::1]:%d", port1)},
					{Addr: fmt.Sprintf("127.0.0.1:%d", port1)},
				}},
				{Addresses: []resolver.Address{
					{Addr: fmt.Sprintf("[::1]:%d", port2)},
					{Addr: fmt.Sprintf("127.0.0.1:%d", port2)},
				}},
				{Addresses: []resolver.Address{
					{Addr: fmt.Sprintf("[::1]:%d", port3)},
					{Addr: fmt.Sprintf("127.0.0.1:%d", port3)},
				}},
			},
		})
		if err != nil {
			log.Fatal("Failed to update resolver state", err)
		}
	}()

	return &exampleResolver{}, nil
}

func (*exampleResolver) Scheme() string {
	return "example"
}

func main() {
	// First send 5 requests using the default DNS and pickfirst load balancer.
	log.Print("**** Use default DNS resolver ****")
	target := fmt.Sprintf("localhost:%d", port1)
	cc, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	client := hwpb.NewGreeterClient(cc)

	for i := 0; i < 5; i++ {
		resp, err := client.SayHello(ctx, &hwpb.HelloRequest{
			Name: fmt.Sprintf("request:%d", i),
		})
		if err != nil {
			log.Panicf("RPC failed: %v", err)
		}
		log.Print("Greeting:", resp.GetMessage())
	}
	cc.Close()

	log.Print("**** Change to use example name resolver ****")
	dOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin":{}}]}`),
	}
	cc, err = grpc.NewClient("example:///ignored", dOpts...)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	client = hwpb.NewGreeterClient(cc)

	// Send 10 requests using the example nameresolver and round robin load
	// balancer. These requests are evenly distributed among the 3 servers
	// rather than favoring the server listening on both addresses because the
	// resolver groups the 3 servers as 3 endpoints each with 2 addresses.
	if err := waitForDistribution(ctx, client); err != nil {
		log.Panic(err)
	}
	log.Print("Successful multiple iterations of 1:1:1 ratio")
}

// waitForDistribution makes RPC's on the greeter client until 3 RPC's follow
// the same 1:1:1 address ratio for the peer. Returns an error if fails to do so
// before context timeout.
func waitForDistribution(ctx context.Context, client hwpb.GreeterClient) error {
	wantPeers := []string{
		// Server 1 is listening on both IPv4 and IPv6 loopback addresses.
		// Since the IPv6 address comes first in the resolver list, it will be
		// given higher priority.
		fmt.Sprintf("[::1]:%d", port1),
		// Server 2 is listening only on the IPv4 loopback address.
		fmt.Sprintf("127.0.0.1:%d", port2),
		// Server 3 is listening only on the IPv6 loopback address.
		fmt.Sprintf("[::1]:%d", port3),
	}
	const iterationsToVerify = 3
	const backendCount = 3
	requestCounter := 0

	for ctx.Err() == nil {
		result := make(map[string]int)
		badRatioSeen := false
		for i := 1; i <= iterationsToVerify && !badRatioSeen; i++ {
			for j := 0; j < backendCount; j++ {
				var peer peer.Peer
				resp, err := client.SayHello(ctx, &hwpb.HelloRequest{
					Name: fmt.Sprintf("request:%d", requestCounter),
				}, grpc.Peer(&peer))
				requestCounter++
				if err != nil {
					return fmt.Errorf("RPC failed: %v", err)
				}
				log.Print("Greeting:", resp.GetMessage())

				peerAddr := peer.Addr.String()
				if !slices.Contains(wantPeers, peerAddr) {
					return fmt.Errorf("peer address was not one of %q, got: %v", strings.Join(wantPeers, ", "), peerAddr)
				}
				result[peerAddr]++
				time.Sleep(time.Millisecond)
			}

			// Verify the results of this iteration.
			for _, count := range result {
				if count == i {
					continue
				}
				badRatioSeen = true
				break
			}
			if !badRatioSeen {
				log.Print("Got iteration with 1:1:1 distribution between addresses.")
			}
		}
		if !badRatioSeen {
			return nil
		}
	}
	return fmt.Errorf("timeout waiting for 1:1:1 distribution between addresses %v", wantPeers)
}
