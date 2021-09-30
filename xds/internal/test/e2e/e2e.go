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
 */

// Package e2e implements xds e2e tests using go-control-plane.
package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"google.golang.org/grpc"
	channelzpb "google.golang.org/grpc/channelz/grpc_channelz_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

func cmd(path string, logger io.Writer, args []string, env []string) (*exec.Cmd, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = logger
	cmd.Stderr = logger
	return cmd, nil
}

const (
	clientStatsPort = 60363 // TODO: make this different per-test, only needed for parallel tests.
)

type client struct {
	cmd *exec.Cmd

	target  string
	statsCC *grpc.ClientConn
}

// newClient create a client with
// - xds bootstrap file at "./configs/bootstrap-<testName>.json" (%s is the test name)
// - xds:///<testName> as the target
func newClient(testName string, binaryPath string, bootstrap string, logger io.Writer, flags ...string) (*client, error) {
	target := fmt.Sprintf("xds:///%s", testName)

	cmd, err := cmd(
		binaryPath,
		logger,
		append([]string{
			"--server=" + target,
			"--print_response=true",
			"--qps=100",
			fmt.Sprintf("--stats_port=%d", clientStatsPort),
		}, flags...), // Append any flags from caller.
		[]string{
			"GRPC_GO_LOG_VERBOSITY_LEVEL=99",
			"GRPC_GO_LOG_SEVERITY_LEVEL=info",
			"GRPC_XDS_BOOTSTRAP_CONFIG=" + bootstrap, // The bootstrap content doesn't need to be quoted.
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run client cmd: %v", err)
	}
	cmd.Start()

	cc, err := grpc.Dial(fmt.Sprintf("localhost:%d", clientStatsPort), grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.WaitForReady(true)))
	if err != nil {
		return nil, err
	}
	return &client{
		cmd:     cmd,
		target:  target,
		statsCC: cc,
	}, nil
}

func (c *client) clientStats(ctx context.Context) (*testpb.LoadBalancerStatsResponse, error) {
	ccc := testpb.NewLoadBalancerStatsServiceClient(c.statsCC)
	return ccc.GetClientStats(ctx, &testpb.LoadBalancerStatsRequest{
		NumRpcs:    100,
		TimeoutSec: 10,
	})
}

func (c *client) configRPCs(ctx context.Context, req *testpb.ClientConfigureRequest) error {
	ccc := testpb.NewXdsUpdateClientConfigureServiceClient(c.statsCC)
	_, err := ccc.Configure(ctx, req)
	return err
}

func (c *client) channelzSubChannels(ctx context.Context) ([]*channelzpb.Subchannel, error) {
	ccc := channelzpb.NewChannelzClient(c.statsCC)
	r, err := ccc.GetTopChannels(ctx, &channelzpb.GetTopChannelsRequest{})
	if err != nil {
		return nil, err
	}

	var ret []*channelzpb.Subchannel
	for _, cc := range r.Channel {
		if cc.Data.Target != c.target {
			continue
		}
		for _, sc := range cc.SubchannelRef {
			rr, err := ccc.GetSubchannel(ctx, &channelzpb.GetSubchannelRequest{SubchannelId: sc.SubchannelId})
			if err != nil {
				return nil, err
			}
			ret = append(ret, rr.Subchannel)
		}
	}
	return ret, nil
}

func (c *client) stop() {
	c.cmd.Process.Kill()
	c.cmd.Wait()
}

const (
	serverPort = 50051 // TODO: make this different per-test, only needed for parallel tests.
)

type server struct {
	cmd  *exec.Cmd
	port int
}

// newServer creates multiple servers with
// - xds bootstrap file at "./configs/bootstrap-<testName>.json" (%s is the test name)
// - <testName>-<index> as the target
func newServers(testName string, binaryPath string, bootstrap string, logger io.Writer, count int) ([]*server, error) {
	var ret []*server
	for i := 0; i < count; i++ {
		port := serverPort + i
		cmd, err := cmd(
			binaryPath,
			logger,
			[]string{fmt.Sprintf("--port=%d", port), fmt.Sprintf("--host_name_override=%s-%d", testName, i)},
			[]string{
				"GRPC_GO_LOG_VERBOSITY_LEVEL=99",
				"GRPC_GO_LOG_SEVERITY_LEVEL=info",
				"GRPC_XDS_BOOTSTRAP_CONFIG=" + bootstrap, // The bootstrap content doesn't need to be quoted.,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to run server cmd: %v", err)
		}
		cmd.Start()
		ret = append(ret, &server{cmd: cmd, port: port})
	}
	return ret, nil
}

func (s *server) stop() {
	s.cmd.Process.Kill()
	s.cmd.Wait()
}
