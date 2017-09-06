/*
 *
 * Copyright 2017 gRPC authors.
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

package grpc

import (
	"math"
	"testing"
	"time"

	"golang.org/x/net/context"
	_ "google.golang.org/grpc/balancer/roundrobin" // To register roundrobin.
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/test/leakcheck"
)

func checkPickFirst(cc *ClientConn, servers []*server, t *testing.T) {
	var (
		req   = "port"
		reply string
		err   error
	)
	// The second RPC should succeed with the first server.
	for i := 0; i < 1000; i++ {
		if err = Invoke(context.Background(), "/foo/bar", &req, &reply, cc); err != nil && ErrorDesc(err) == servers[0].port {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("EmptyCall() = _, %v, want _, %v", err, servers[0].port)
}

func checkRoundRobin(cc *ClientConn, servers []*server, t *testing.T) {
	var (
		req   = "port"
		reply string
		err   error
	)

	// Make sure connections to all servers are up.
	for _, s := range servers {
		for {
			if err = Invoke(context.Background(), "/foo/bar", &req, &reply, cc); err != nil && ErrorDesc(err) == s.port {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}

	serverCount := len(servers)
	for i := 0; i < 3*serverCount; i++ {
		err = Invoke(context.Background(), "/foo/bar", &req, &reply, cc)
		if ErrorDesc(err) != servers[i%serverCount].port {
			t.Fatalf("Index %d: want peer %v, got peer %v", i, servers[i%serverCount].port, err)
		}
	}
}

func TestSwitchBalancer(t *testing.T) {
	defer leakcheck.Check(t)
	r, rcleanup := manual.GenerateAndRegisterManualResolver()
	defer rcleanup()

	numServers := 2
	servers, _, scleanup := startServers(t, numServers, math.MaxInt32)
	defer scleanup()

	cc, err := Dial(r.Scheme()+":///test.server", WithInsecure(), WithCodec(testCodec{}))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer cc.Close()
	r.NewAddress([]resolver.Address{{Addr: servers[0].addr}, {Addr: servers[1].addr}})
	// The default balancer is pickfirst.
	checkPickFirst(cc, servers, t)
	// Switch to roundrobin.
	cc.switchBalancer("roundrobin")
	checkRoundRobin(cc, servers, t)
	// Switch to pickfirst.
	cc.switchBalancer("pickfirst")
	checkPickFirst(cc, servers, t)
}
