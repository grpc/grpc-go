/*
 *
 * Copyright 2024 gRPC authors.
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

package grpc_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/balancer/stub"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// TestProducerStopsBeforeStateChange confirms that producers are stopped before
// any state change notification is delivered to the LB policy.
func (s) TestProducerStopsBeforeStateChange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	name := strings.ReplaceAll(strings.ToLower(t.Name()), "/", "")
	var lastProducer *testProducer
	bf := stub.BalancerFuncs{
		UpdateClientConnState: func(bd *stub.BalancerData, ccs balancer.ClientConnState) error {
			var sc balancer.SubConn
			sc, err := bd.ClientConn.NewSubConn(ccs.ResolverState.Addresses, balancer.NewSubConnOptions{
				StateListener: func(scs balancer.SubConnState) {
					bd.ClientConn.UpdateState(balancer.State{
						ConnectivityState: scs.ConnectivityState,
						// We do not pass a picker, but since we don't perform
						// RPCs, that's okay.
					})
					if !lastProducer.stopped.Load() {
						t.Errorf("lastProducer not stopped before state change notification")
					}
					t.Logf("State is now %v; recreating producer", scs.ConnectivityState)
					p, _ := sc.GetOrBuildProducer(producerBuilderSingleton)
					lastProducer = p.(*testProducer)
				},
			})
			if err != nil {
				return err
			}
			p, _ := sc.GetOrBuildProducer(producerBuilderSingleton)
			lastProducer = p.(*testProducer)
			sc.Connect()
			return nil
		},
	}
	stub.Register(name, bf)

	ss := stubserver.StubServer{
		FullDuplexCallF: func(stream testgrpc.TestService_FullDuplexCallServer) error {
			return nil
		},
	}
	if err := ss.StartServer(); err != nil {
		t.Fatal("Error starting server:", err)
	}
	defer ss.Stop()

	cc, err := grpc.NewClient("dns:///"+ss.Address,
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"`+name+`":{}}]}`),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer cc.Close()

	go cc.Connect()
	testutils.AwaitState(ctx, t, cc, connectivity.Ready)

	cc.Close()
	testutils.AwaitState(ctx, t, cc, connectivity.Shutdown)
}

type producerBuilder struct{}

type testProducer struct {
	// There should be no race accessing this field, but use an atomic since
	// the race checker probably can't detect that.
	stopped atomic.Bool
}

// Build constructs and returns a producer and its cleanup function
func (*producerBuilder) Build(cci any) (balancer.Producer, func()) {
	p := &testProducer{}
	return p, func() {
		p.stopped.Store(true)
	}
}

var producerBuilderSingleton = &producerBuilder{}
