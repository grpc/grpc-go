/*
 *
 * Copyright 2023 gRPC authors.
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

// Binary client is a client for the custom load balancer example.
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
	_ "google.golang.org/grpc/examples/features/customloadbalancer/client/customroundrobin" // To register custom_round_robin.
	pb "google.golang.org/grpc/examples/features/proto/echo"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

const (
	port1       = 50050
	port2       = 50051
	port3       = 50052
	repeatCount = 3
)

func main() {
	mr := manual.NewBuilderWithScheme("example")
	defer mr.Close()

	// You can also plug in your own custom lb policy, which needs to be
	// configurable. This "repeatCount" is configurable. Try changing it and
	// see how the behavior changes.
	json := fmt.Sprintf(`{"loadBalancingConfig": [{"custom_round_robin":{"repeatCount": %d}}]}`, repeatCount)
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	// The resolver will produce both IPv4 and IPv6 addresses for each server.
	// The leaf pickfirst balancers will handle connection attempts to each
	// address within an endpoint.
	mr.InitialState(resolver.State{
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
		ServiceConfig: sc,
	})

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ec := pb.NewEchoClient(cc)
	if err := waitForDistribution(ctx, ec); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Successful multiple iterations of 1:1:1 ratio")
}

// waitForDistribution makes RPC's on the echo client until 9 RPC's follow the
// same 1:1:1 address ratio for the peer. Returns an error if fails to do so
// before context timeout.
func waitForDistribution(ctx context.Context, ec pb.EchoClient) error {
	wantPeers := []string{
		// Server 1 is listening only on the IPv4 loopback address.
		fmt.Sprintf("127.0.0.1:%d", port1),
		// Server 2 is listening only on the IPv6 loopback address.
		fmt.Sprintf("[::1]:%d", port2),
		// Server 3 is listening on both IPv4 and IPv6 loopback addresses.
		// Since the IPv6 address is first in the list, it will be given
		// higher priority.
		fmt.Sprintf("[::1]:%d", port3),
	}
	for {
		results := make(map[string]uint32)
	InnerLoop:
		for {
			if ctx.Err() != nil {
				return fmt.Errorf("timeout waiting for 1:1:1 distribution between addresses %v", wantPeers)
			}

			for i := 0; i < 3; i++ {
				res := make(map[string]uint32)
				for j := 0; j < 3; j++ {
					prevAddr := ""
					for repeat := 0; repeat < repeatCount; repeat++ {
						var peer peer.Peer
						r, err := ec.UnaryEcho(ctx, &pb.EchoRequest{Message: "this is examples/customloadbalancing"}, grpc.Peer(&peer))
						if err != nil {
							return fmt.Errorf("UnaryEcho failed: %v", err)
						}
						peerAddr := peer.Addr.String()
						fmt.Printf("Response from peer %q: %v\n", peerAddr, r)
						if !slices.Contains(wantPeers, peerAddr) {
							return fmt.Errorf("peer address was not one of %q, got: %v", strings.Join(wantPeers, ", "), peerAddr)
						}
						// We expect to see the peer repeated "repeatCount"
						// times.
						if prevAddr != "" && peerAddr != prevAddr {
							break InnerLoop
						}
						prevAddr = peerAddr
						res[peerAddr]++
						time.Sleep(time.Millisecond)
					}
				}
				// Make sure the addresses come in a 1:1:1 ratio for this
				// iteration.
				for _, count := range res {
					if count != repeatCount {
						break InnerLoop
					}
				}
			}
			// Make sure iteration is 9 for addresses seen. This makes sure the
			// distribution is the same 1:1:1 ratio for each iteration.
			for _, count := range results {
				if count != 3*repeatCount {
					break InnerLoop
				}
			}
			return nil
		}
	}
}
