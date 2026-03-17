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

var (
	addr1 = "localhost:50050"
	addr2 = "localhost:50051"
)

func main() {
	mr := manual.NewBuilderWithScheme("example")
	defer mr.Close()

	// You can also plug in your own custom lb policy, which needs to be
	// configurable. This n is configurable. Try changing n and see how the
	// behavior changes.
	json := `{"loadBalancingConfig": [{"custom_round_robin":{"chooseSecond": 3}}]}`
	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(json)
	mr.InitialState(resolver.State{
		Endpoints: []resolver.Endpoint{
			{Addresses: []resolver.Address{{Addr: addr1}}},
			{Addresses: []resolver.Address{{Addr: addr2}}},
		},
		ServiceConfig: sc,
	})

	cc, err := grpc.NewClient(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("grpc.NewClient() failed: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	ec := pb.NewEchoClient(cc)
	if err := waitForDistribution(ctx, ec); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Successful multiple iterations of 1:2 ratio")
}

// waitForDistribution makes RPC's on the echo client until 3 RPC's follow the
// same 1:2 address ratio for the peer. Returns an error if fails to do so
// before context timeout.
func waitForDistribution(ctx context.Context, ec pb.EchoClient) error {
	for {
		results := make(map[string]uint32)
	InnerLoop:
		for {
			if ctx.Err() != nil {
				return fmt.Errorf("timeout waiting for 1:2 distribution between addresses %v and %v", addr1, addr2)
			}

			for i := 0; i < 3; i++ {
				res := make(map[string]uint32)
				for j := 0; j < 3; j++ {
					var peer peer.Peer
					r, err := ec.UnaryEcho(ctx, &pb.EchoRequest{Message: "this is examples/customloadbalancing"}, grpc.Peer(&peer))
					if err != nil {
						return fmt.Errorf("UnaryEcho failed: %v", err)
					}
					fmt.Println(r)
					peerAddr := peer.Addr.String()
					if !strings.HasSuffix(peerAddr, "50050") && !strings.HasSuffix(peerAddr, "50051") {
						return fmt.Errorf("peer address was not one of %v or %v, got: %v", addr1, addr2, peerAddr)
					}
					res[peerAddr]++
					time.Sleep(time.Millisecond)
				}
				// Make sure the addresses come in a 1:2 ratio for this
				// iteration.
				var seen1, seen2 bool
				for addr, count := range res {
					if count != 1 && count != 2 {
						break InnerLoop
					}
					if count == 1 {
						if seen1 {
							break InnerLoop
						}
						seen1 = true
					}
					if count == 2 {
						if seen2 {
							break InnerLoop
						}
						seen2 = true
					}
					results[addr] = results[addr] + count
				}
				if !seen1 || !seen2 {
					break InnerLoop
				}
			}
			// Make sure iteration is 3 and 6 for addresses seen. This makes
			// sure the distribution is the same 1:2 ratio for each iteration.
			var seen3, seen6 bool
			for _, count := range results {
				if count != 3 && count != 6 {
					break InnerLoop
				}
				if count == 3 {
					if seen3 {
						break InnerLoop
					}
					seen3 = true
				}
				if count == 6 {
					if seen6 {
						break InnerLoop
					}
					seen6 = true
				}
				return nil
			}
			if !seen3 || !seen6 {
				break InnerLoop
			}
		}
	}
}
