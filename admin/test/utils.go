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

// Package test contains test only functions for package admin. It's used by
// admin/admin_test.go and admin/test/admin_test.go in this directory.
package test

import (
	"context"
	"fmt"
	"net"
	"time"

	v3statuspb "github.com/envoyproxy/go-control-plane/envoy/service/status/v3"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/admin"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/xds"
	"google.golang.org/grpc/status"
)

const (
	defaultTestTimeout = 10 * time.Second
)

// RunAndCode contains the function to make an RPC and the expected status code
// (can be OK).
type RunAndCode struct {
	// Name of this RPC.
	Name string
	// The function to make the RPC on the ClientConn.
	Run func(*grpc.ClientConn) error
	// The expected code returned from the RPC.
	Code codes.Code
}

// RunRegisterTests makes a client, runs the RPCs, and compares the status
// codes.
func RunRegisterTests(rcs []RunAndCode) error {
	nodeID := uuid.New().String()
	bootstrapCleanup, err := xds.SetupBootstrapFile(xds.BootstrapOptions{
		Version:   xds.TransportV3,
		NodeID:    nodeID,
		ServerURI: "no.need.for.a.server",
	})
	if err != nil {
		return err
	}
	defer bootstrapCleanup()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("cannot create listener: %v", err)
	}

	server := grpc.NewServer()
	defer server.Stop()
	cleanup, err := admin.Register(server)
	if err != nil {
		return fmt.Errorf("failed to register admin: %v", err)
	}
	defer cleanup()
	go func() {
		server.Serve(lis)
	}()

	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("cannot connect to server: %v", err)
	}

	for _, rc := range rcs {
		if err := rc.Run(conn); status.Code(err) != rc.Code {
			return fmt.Errorf("%s test failed with error %v, want code %v", rc.Name, err, rc.Code)
		}
	}
	return nil
}

// RunChannelz makes a channelz RPC.
func RunChannelz(conn *grpc.ClientConn) error {
	c := channelzpb.NewChannelzClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := c.GetTopChannels(ctx, &channelzpb.GetTopChannelsRequest{}, grpc.WaitForReady(true))
	return err
}

// RunCSDS makes a CSDS RPC.
func RunCSDS(conn *grpc.ClientConn) error {
	c := v3statuspb.NewClientStatusDiscoveryServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_, err := c.FetchClientStatus(ctx, &v3statuspb.ClientStatusRequest{}, grpc.WaitForReady(true))
	return err
}
