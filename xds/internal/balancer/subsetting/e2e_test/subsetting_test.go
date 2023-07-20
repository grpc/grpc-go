// Package e2e_test contains e2e test cases for the Subsetting LB Policy.
package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	_ "google.golang.org/grpc/xds/internal/balancer/subsetting"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func setupBackends(t *testing.T, backendsCount int) ([]resolver.Address, func()) {
	t.Helper()

	backends := make([]*stubserver.StubServer, backendsCount)
	addresses := make([]resolver.Address, backendsCount)
	for i := 0; i < backendsCount; i++ {
		backend := &stubserver.StubServer{
			EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
				return &testpb.Empty{}, nil
			},
		}
		if err := backend.StartServer(); err != nil {
			t.Fatalf("Failed to start backend: %v", err)
		}
		t.Logf("Started good TestService backend at: %q", backend.Address)
		backends[i] = backend
		addresses[i] = resolver.Address{
			Addr: backend.Address,
		}
	}

	cancel := func() {
		for _, backend := range backends {
			backend.Stop()
		}
	}
	return addresses, cancel
}

func setupClients(t *testing.T, clientsCount int, subsetSize int, addresses []resolver.Address) ([]testgrpc.TestServiceClient, func()) {
	t.Helper()

	clients := make([]testgrpc.TestServiceClient, clientsCount)
	ccs := make([]*grpc.ClientConn, clientsCount)
	var err error

	for i := 0; i < clientsCount; i++ {
		mr := manual.NewBuilderWithScheme("subsetting-e2e")
		jsonConfig := fmt.Sprintf(`
		{
		  "loadBalancingConfig": [
			{
			  "subsetting_experimental": {
				"clientIndex": %d,
				"subsetSize": %d,
				"childPolicy": [{"round_robin": {}}]
			  }
			}
		  ]
		}`, i, subsetSize)

		sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(string(jsonConfig))
		mr.InitialState(resolver.State{
			Addresses:     addresses,
			ServiceConfig: sc,
		})

		ccs[i], err = grpc.Dial(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			t.Fatalf("grpc.Dial() failed: %v", err)
		}
		clients[i] = testgrpc.NewTestServiceClient(ccs[i])
	}

	cancel := func() {
		for _, cc := range ccs {
			cc.Close()
		}
	}
	return clients, cancel
}

func checkRoundRobinRPCs(t *testing.T, ctx context.Context, clients []testgrpc.TestServiceClient, reqPerClient, expectedClientsPerBackend, expectedReqPerBackend int) {
	reqPerBackend := map[string]int{}
	clientsPerBackend := map[string]map[int]struct{}{}

	for clientIdx, client := range clients {
		for i := 0; i < reqPerClient; i++ {
			var peer peer.Peer
			_, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(&peer))
			if err != nil {
				t.Fatalf("failed to call server: %v", err)
			}
			if peer.Addr != nil {
				reqPerBackend[peer.Addr.String()]++
				if m, ok := clientsPerBackend[peer.Addr.String()]; !ok {
					clientsPerBackend[peer.Addr.String()] = map[int]struct{}{clientIdx: struct{}{}}
				} else {
					m[clientIdx] = struct{}{}
				}
			}
		}
	}

	for k, v := range reqPerBackend {
		if v != expectedReqPerBackend {
			t.Fatalf("got unepected number of req per backend, expected : %d, actual: %d, backend: %s", expectedReqPerBackend, v, k)
		}
	}

	for k, v := range clientsPerBackend {
		if len(v) != expectedClientsPerBackend {
			t.Fatalf("got unepected number of clients per backend, expected : %d, actual: %d, backend: %s", expectedReqPerBackend, len(v), k)
		}
	}
}

func (s) TestSubsettingE2E(t *testing.T) {
	tests := []struct {
		name       string
		subsetSize int
		clients    int
		backends   int
	}{
		{
			name:       "backends could be evenly distributed between clients",
			backends:   12,
			clients:    8,
			subsetSize: 3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			addresses, stopBackends := setupBackends(t, test.backends)
			defer stopBackends()

			clients, stopClients := setupClients(t, test.clients, test.subsetSize, addresses)
			defer stopClients()

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()

			requPerClient := 2 * test.subsetSize
			expectedClientsPerBackend := test.clients * test.subsetSize / test.backends
			expectedReqPerBackend := 2 * expectedClientsPerBackend
			checkRoundRobinRPCs(t, ctx, clients, requPerClient, expectedClientsPerBackend, expectedReqPerBackend)
		})
	}
}
