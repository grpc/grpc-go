/*
 *
 * Copyright 2019 gRPC authors.
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
	"net"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
)

type client struct {
	cc           *grpc.ClientConn
	cancel       context.CancelFunc
	balancerName string
	serviceName  string
	enableCDS    bool
	opts         balancer.BuildOptions
	newEDS       func(assignment *api.ClusterLoadAssignment) error
	newCDS       func(cluster *api.Cluster) error
	loseContact  func()
	closeChan    chan struct{}
}

func (c *client) Run() {
	c.loseContact()
	c.dial()
	c.makeADSCall()
}

func (c *client) Close() {
	select {
	case <-c.closeChan:
	default:
		close(c.closeChan)
		c.cancel()
	}
}

func (c *client) dial() {
	var dopts []grpc.DialOption
	if creds := c.opts.DialCreds; creds != nil {
		if err := creds.OverrideServerName(c.balancerName); err == nil {
			dopts = append(dopts, grpc.WithTransportCredentials(creds))
		} else {
			grpclog.Warningf("xds: failed to override the server name in the credentials: %v, using Insecure", err)
			dopts = append(dopts, grpc.WithInsecure())
		}
	} else if bundle := c.grpclbClientConnCreds; bundle != nil {
		dopts = append(dopts, grpc.WithCredentialsBundle(bundle))
	} else {
		dopts = append(dopts, grpc.WithInsecure())
	}
	if c.opts.Dialer != nil {
		// WithDialer takes a different type of function, so we instead use a
		// special DialOption here.
		wcd := internal.WithContextDialer.(func(func(context.Context, string) (net.Conn, error)) grpc.DialOption)
		dopts = append(dopts, wcd(c.opts.Dialer))
	}
	// Explicitly set pickfirst as the balancer.
	dopts = append(dopts, grpc.WithBalancerName(grpc.PickFirstBalancerName))
	if channelz.IsOn() {
		dopts = append(dopts, grpc.WithChannelzParentID(c.opts.ChannelzParentID))
	}

	// DialContext using manualResolver.Scheme, which is a random scheme
	// generated when init grpclb. The target scheme here is not important.
	//
	// The grpc dial target will be used by the creds (ALTS) as the authority,
	// so it has to be set to remoteLBName that comes from resolver.
	var err error
	c.cc, err = grpc.DialContext(context.Background(), c.balancerName, dopts...)
	if err != nil {
		grpclog.Fatalf("failed to dial: %v", err)
	}
}

func (c *client) newCDSRequest() *api.DiscoveryRequest {
	cdsReq := &api.DiscoveryRequest{
		Node: &core.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					"com.googleapis.trafficdirector.grpc_hostname": {
						Kind: &types.Value_StringValue{StringValue: c.serviceName},
					},
				},
			},
		},
		TypeUrl: "type.googleapis.com/envoy.api.v2.Cluster",
	}
	return cdsReq
}

func (c *client) newEDSRequest(endpointRequired bool) *api.DiscoveryRequest {
	edsReq := &api.DiscoveryRequest{
		Node: &core.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					"endpoints_required": {
						Kind: &types.Value_BoolValue{BoolValue: endpointRequired},
					},
				},
			},
		},
		ResourceNames: []string{c.serviceName},
		TypeUrl:       "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment",
	}
	return edsReq
}

func (c *client) makeADSCall() {
	cli := discovery.NewAggregatedDiscoveryServiceClient(c.cc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.cancel = cancel
	for ; ; c.loseContact() {
		select {
		case <-ctx.Done():
			break
		}
		sctx, scancel := context.WithCancel(ctx)
		st, err := cli.StreamAggregatedResources(sctx, grpc.WaitForReady(true))
		if err != nil {
			grpclog.Infof("xds: failed to initial ADS streaming RPC due to %v", err)
			continue
		}
		if c.enableCDS {
			if err := st.Send(c.newCDSRequest()); err != nil {
				// current stream is broken, start a new one.
				continue
			}
			if err := st.Send(c.newEDSRequest(true)); err != nil {
				// current stream is broken, start a new one.
				continue
			}
		} else {
			if err := st.Send(c.newEDSRequest(false)); err != nil {
				// current stream is broken, start a new one.
				continue
			}
		}
		for {
			resp, err := st.Recv()
			if err != nil {
				// current stream is broken, start a new one.
				break
			}
			resources := resp.GetResources()
			if len(resources) < 1 {
				grpclog.Warning("xds: ADS response contains 0 resource info.")
				// cancel this RPC as server misbehaves by sending a ADS response with 0 resource info.
				scancel()
				break
			}
			switch resp.GetTypeUrl() {
			case "type.googleapis.com/envoy.api.v2.Cluster":
				cdsResp := &api.Cluster{}
				if err := proto.Unmarshal(resources[0].GetValue(), cdsResp); err != nil {
					scancel()
					break
				}
				if err := c.newCDS(cdsResp); err != nil {
					scancel()
					break
				}
			case "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment":
				edsResp := &api.ClusterLoadAssignment{}
				if err := proto.Unmarshal(resources[0].GetValue(), edsResp); err != nil {
					scancel()
					break
				}
				if err := c.newEDS(edsResp); err != nil {
					scancel()
					break
				}
			default:
				grpclog.Warningf("xds: received response type not expected, type: %T, %v", resp, resp)
			}
		}
	}
}

func newClient(balancerName string, serviceName string, enableCDS bool, opts balancer.BuildOptions, newCDS func(*api.Cluster) error, newEDS func(*api.ClusterLoadAssignment) error, loseContact func()) *client {
	return &client{
		balancerName: balancerName,
		serviceName:  serviceName,
		enableCDS:    enableCDS,
		opts:         opts,
		newEDS:       newEDS,
		newCDS:       newCDS,
		loseContact:  loseContact,
		closeChan:    make(chan struct{}),
	}
}
