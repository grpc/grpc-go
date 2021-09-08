//go:build !386
// +build !386

/*
 *
 * Copyright 2021 gRPC authors.
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

package xds_test

import (
	"context"
	"fmt"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/xds/env"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/testutils"
	"google.golang.org/grpc/xds/internal/testutils/e2e"

	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

// clientSetup performs a bunch of steps common to all xDS client tests here:
// - spin up a gRPC server and register the test service on it
// - create a local TCP listener and start serving on it
//
// Returns the following:
// - the port the server is listening on
// - cleanup function to be invoked by the tests when done
func clientSetup(t *testing.T, tss testpb.TestServiceServer) (uint32, func()) {
	// Initialize a gRPC server and register the stubServer on it.
	server := grpc.NewServer()
	testpb.RegisterTestServiceServer(server, tss)

	// Create a local listener and pass it to Serve().
	lis, err := testutils.LocalTCPListener()
	if err != nil {
		t.Fatalf("testutils.LocalTCPListener() failed: %v", err)
	}

	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("Serve() failed: %v", err)
		}
	}()

	return uint32(lis.Addr().(*net.TCPAddr).Port), func() {
		server.Stop()
	}
}

func (s) TestClientSideXDS(t *testing.T) {
	managementServer, nodeID, _, resolver, cleanup1 := setupManagementServer(t)
	defer cleanup1()

	port, cleanup2 := clientSetup(t, &testService{})
	defer cleanup2()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("rpc EmptyCall() failed: %v", err)
	}
}

func (s) TestClientSideRetry(t *testing.T) {
	if !env.RetrySupport {
		// Skip this test if retry is not enabled.
		return
	}

	ctr := 0
	errs := []codes.Code{codes.ResourceExhausted}
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			defer func() { ctr++ }()
			if ctr < len(errs) {
				return nil, status.Errorf(errs[ctr], "this should be retried")
			}
			return &testpb.Empty{}, nil
		},
	}

	managementServer, nodeID, _, resolver, cleanup1 := setupManagementServer(t)
	defer cleanup1()

	port, cleanup2 := clientSetup(t, ss)
	defer cleanup2()

	const serviceName = "my-service-client-side-xds"
	resources := e2e.DefaultClientResources(e2e.ResourceParams{
		DialTarget: serviceName,
		NodeID:     nodeID,
		Host:       "localhost",
		Port:       port,
		SecLevel:   e2e.SecurityLevelNone,
	})
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	if err := managementServer.Update(ctx, resources); err != nil {
		t.Fatal(err)
	}

	// Create a ClientConn and make a successful RPC.
	cc, err := grpc.Dial(fmt.Sprintf("xds:///%s", serviceName), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithResolvers(resolver))
	if err != nil {
		t.Fatalf("failed to dial local test server: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)
	defer cancel()
	if _, err := client.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("rpc EmptyCall() = _, %v; want _, ResourceExhausted", err)
	}

	testCases := []struct {
		name        string
		vhPolicy    *v3routepb.RetryPolicy
		routePolicy *v3routepb.RetryPolicy
		errs        []codes.Code // the errors returned by the server for each RPC
		tryAgainErr codes.Code   // the error that would be returned if we are still using the old retry policies.
		errWant     codes.Code
	}{{
		name: "virtualHost only, fail",
		vhPolicy: &v3routepb.RetryPolicy{
			RetryOn:    "resource-exhausted,unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 1},
		},
		errs:        []codes.Code{codes.ResourceExhausted, codes.Unavailable},
		routePolicy: nil,
		tryAgainErr: codes.ResourceExhausted,
		errWant:     codes.Unavailable,
	}, {
		name: "virtualHost only",
		vhPolicy: &v3routepb.RetryPolicy{
			RetryOn:    "resource-exhausted, unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		errs:        []codes.Code{codes.ResourceExhausted, codes.Unavailable},
		routePolicy: nil,
		tryAgainErr: codes.Unavailable,
		errWant:     codes.OK,
	}, {
		name: "virtualHost+route, fail",
		vhPolicy: &v3routepb.RetryPolicy{
			RetryOn:    "resource-exhausted,unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		routePolicy: &v3routepb.RetryPolicy{
			RetryOn:    "resource-exhausted",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		errs:        []codes.Code{codes.ResourceExhausted, codes.Unavailable},
		tryAgainErr: codes.OK,
		errWant:     codes.Unavailable,
	}, {
		name: "virtualHost+route",
		vhPolicy: &v3routepb.RetryPolicy{
			RetryOn:    "resource-exhausted",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		routePolicy: &v3routepb.RetryPolicy{
			RetryOn:    "unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		errs:        []codes.Code{codes.Unavailable},
		tryAgainErr: codes.Unavailable,
		errWant:     codes.OK,
	}, {
		name: "virtualHost+route, not enough attempts",
		vhPolicy: &v3routepb.RetryPolicy{
			RetryOn:    "unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 2},
		},
		routePolicy: &v3routepb.RetryPolicy{
			RetryOn:    "unavailable",
			NumRetries: &wrapperspb.UInt32Value{Value: 1},
		},
		errs:        []codes.Code{codes.Unavailable, codes.Unavailable},
		tryAgainErr: codes.OK,
		errWant:     codes.Unavailable,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs = tc.errs

			// Confirm tryAgainErr is correct before updating resources.
			ctr = 0
			_, err := client.EmptyCall(ctx, &testpb.Empty{})
			if code := status.Code(err); code != tc.tryAgainErr {
				t.Fatalf("with old retry policy: EmptyCall() = _, %v; want _, %v", err, tc.tryAgainErr)
			}

			resources.Routes[0].VirtualHosts[0].RetryPolicy = tc.vhPolicy
			resources.Routes[0].VirtualHosts[0].Routes[0].GetRoute().RetryPolicy = tc.routePolicy
			if err := managementServer.Update(ctx, resources); err != nil {
				t.Fatal(err)
			}

			for {
				ctr = 0
				_, err := client.EmptyCall(ctx, &testpb.Empty{})
				if code := status.Code(err); code == tc.tryAgainErr {
					continue
				} else if code != tc.errWant {
					t.Fatalf("rpc EmptyCall() = _, %v; want _, %v", err, tc.errWant)
				}
				break
			}
		})
	}
}
