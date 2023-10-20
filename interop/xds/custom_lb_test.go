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

package xds

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/serviceconfig"
)

var defaultTestTimeout = 5 * time.Second

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// TestCustomLB tests the Custom LB for the interop client. It configures the
// custom lb as the top level Load Balancing policy of the channel, then asserts
// it can successfully make an RPC and also that the rpc behavior the Custom LB
// is configured with makes it's way to the server in metadata.
func (s) TestCustomLB(t *testing.T) {
	errCh := testutils.NewChannel()
	// Setup a backend which verifies the expected rpc-behavior metadata is
	// present in the request.
	backend := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				errCh.Send(errors.New("failed to receive metadata"))
				return &testpb.SimpleResponse{}, nil
			}
			rpcBMD := md.Get("rpc-behavior")
			if len(rpcBMD) != 1 {
				errCh.Send(fmt.Errorf("received %d values for metadata key \"rpc-behavior\", want 1", len(rpcBMD)))
				return &testpb.SimpleResponse{}, nil
			}
			wantVal := "error-code-0"
			if rpcBMD[0] != wantVal {
				errCh.Send(fmt.Errorf("metadata val for key \"rpc-behavior\": got val %v, want val %v", rpcBMD[0], wantVal))
				return &testpb.SimpleResponse{}, nil
			}
			// Success.
			errCh.Send(nil)
			return &testpb.SimpleResponse{}, nil
		},
	}
	if err := backend.StartServer(); err != nil {
		t.Fatalf("Failed to start backend: %v", err)
	}
	t.Logf("Started good TestService backend at: %q", backend.Address)
	defer backend.Stop()

	lbCfgJSON := `{
  		"loadBalancingConfig": [
    		{
      			"test.RpcBehaviorLoadBalancer": {
					"rpcBehavior": "error-code-0"
      		}
    	}
  	]
	}`

	sc := internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(lbCfgJSON)
	mr := manual.NewBuilderWithScheme("customlb-e2e")
	defer mr.Close()
	mr.InitialState(resolver.State{
		Addresses: []resolver.Address{
			{Addr: backend.Address},
		},
		ServiceConfig: sc,
	})

	cc, err := grpc.Dial(mr.Scheme()+":///", grpc.WithResolvers(mr), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial() failed: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	testServiceClient := testgrpc.NewTestServiceClient(cc)

	// Make a Unary RPC. This RPC should be successful due to the round_robin
	// leaf balancer. Also, the custom load balancer should inject the
	// "rpc-behavior" string it is configured with into the metadata sent to
	// server.
	if _, err := testServiceClient.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("EmptyCall() failed: %v", err)
	}

	val, err := errCh.Receive(ctx)
	if err != nil {
		t.Fatalf("error receiving from errCh: %v", err)
	}

	// Should receive nil on the error channel which implies backend verified it
	// correctly received the correct "rpc-behavior" metadata.
	if err, ok := val.(error); ok {
		t.Fatalf("error in backend verifications on metadata received: %v", err)
	}
}
