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

// Binary client is an example client.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/orca"

	v3orcapb "github.com/cncf/xds/go/xds/data/orca/v3"
	pb "google.golang.org/grpc/examples/features/proto/echo"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")
var test = flag.Bool("test", false, "if set, only 1 RPC is performed before exiting")

func main() {
	flag.Parse()

	// Set up a connection to the server.  Configure to use our custom LB
	// policy which will receive all the ORCA load reports.
	conn, err := grpc.Dial(*addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"orca_example":{}}]}`),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewEchoClient(conn)

	// Perform RPCs once per second.
	ticker := time.NewTicker(time.Second)
	for range ticker.C {
		func() {
			// Use an anonymous function to ensure context cancelation via defer.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := c.UnaryEcho(ctx, &pb.EchoRequest{Message: "test echo message"}); err != nil {
				log.Fatalf("Error from UnaryEcho call: %v", err)
			}
		}()
		if *test {
			return
		}
	}

}

// Register an ORCA load balancing policy to receive per-call metrics and
// out-of-band metrics.
func init() {
	balancer.Register(orcaLBBuilder{})
}

type orcaLBBuilder struct{}

func (orcaLBBuilder) Name() string { return "orca_example" }
func (orcaLBBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &orcaLB{cc: cc}
}

// orcaLB is an incomplete LB policy designed to show basic ORCA load reporting
// functionality.  It collects per-call metrics in the `Done` callback returned
// by its picker, and it collects out-of-band metrics by registering a listener
// when its SubConn is created.  It does not follow general LB policy best
// practices and makes assumptions about the simple test environment it is
// designed to run within.
type orcaLB struct {
	cc balancer.ClientConn
}

func (o *orcaLB) UpdateClientConnState(ccs balancer.ClientConnState) error {
	// We assume only one update, ever, containing exactly one address, given
	// the use of the "passthrough" (default) name resolver.

	addrs := ccs.ResolverState.Addresses
	if len(addrs) != 1 {
		return fmt.Errorf("orcaLB: expected 1 address; received: %v", addrs)
	}

	// Create one SubConn for the address and connect it.
	var sc balancer.SubConn
	sc, err := o.cc.NewSubConn(addrs, balancer.NewSubConnOptions{
		StateListener: func(scs balancer.SubConnState) {
			if scs.ConnectivityState == connectivity.Ready {
				o.cc.UpdateState(balancer.State{ConnectivityState: connectivity.Ready, Picker: &picker{sc}})
			}
		},
	})
	if err != nil {
		return fmt.Errorf("orcaLB: error creating SubConn: %v", err)
	}
	sc.Connect()

	// Register a simple ORCA OOB listener on the SubConn.  We request a 1
	// second report interval, but in this example the server indicated the
	// minimum interval it will allow is 3 seconds, so reports will only be
	// sent that often.
	orca.RegisterOOBListener(sc, orcaLis{}, orca.OOBListenerOptions{ReportInterval: time.Second})

	return nil
}

func (o *orcaLB) ResolverError(error) {}

// TODO: unused; remove when no longer required.
func (o *orcaLB) UpdateSubConnState(sc balancer.SubConn, scs balancer.SubConnState) {}

func (o *orcaLB) Close() {}

type picker struct {
	sc balancer.SubConn
}

func (p *picker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	return balancer.PickResult{
		SubConn: p.sc,
		Done: func(di balancer.DoneInfo) {
			fmt.Println("Per-call load report received:", di.ServerLoad.(*v3orcapb.OrcaLoadReport).GetRequestCost())
		},
	}, nil
}

// orcaLis is the out-of-band load report listener that we pass to
// orca.RegisterOOBListener to receive periodic load report information.
type orcaLis struct{}

func (orcaLis) OnLoadReport(lr *v3orcapb.OrcaLoadReport) {
	fmt.Println("Out-of-band load report received:", lr)
}
