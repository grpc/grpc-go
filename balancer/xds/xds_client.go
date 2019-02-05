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

	xdspb "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscorepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	xdsdiscoverypb "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
)

const (
	grpcHostname = "com.googleapis.trafficdirector.grpc_hostname"
	cdsType      = "type.googleapis.com/envoy.xdspb.v2.Cluster"
	edsType      = "type.googleapis.com/envoy.xdspb.v2.ClusterLoadAssignment"
)

type client struct {
	cc                 *grpc.ClientConn
	ctx                context.Context
	cancel             context.CancelFunc
	opts               balancer.BuildOptions
	balancerName       string // the traffic director name
	serviceName        string // the user dial target name
	xdsClientConnCreds credentials.Bundle
	xdsBackendCreds    credentials.Bundle
	enableCDS          bool
	newADS             func(resp interface{}) error
	loseContact        func(startup bool)
}

func (c *client) run() {
	c.loseContact(true)
	c.dial()
	c.makeADSCall()
}

func (c *client) close() {
	c.cancel()
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
	} else if bundle := c.xdsClientConnCreds; bundle != nil {
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

	var err error
	c.cc, err = grpc.DialContext(c.ctx, c.balancerName, dopts...)
	// Since this is a non-blocking dial, so if it fails, it due to some serious error (not network
	// related) error.
	if err != nil {
		grpclog.Fatalf("failed to dial: %v", err)
	}
}

func (c *client) newCDSRequest() *xdspb.DiscoveryRequest {
	cdsReq := &xdspb.DiscoveryRequest{
		Node: &xdscorepb.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					grpcHostname: {
						Kind: &types.Value_StringValue{StringValue: c.serviceName},
					},
				},
			},
		},
		TypeUrl: cdsType,
	}
	return cdsReq
}

func (c *client) newEDSRequest() *xdspb.DiscoveryRequest {
	edsReq := &xdspb.DiscoveryRequest{
		Node: &xdscorepb.Node{
			Metadata: &types.Struct{
				Fields: map[string]*types.Value{
					"endpoints_required": {
						Kind: &types.Value_BoolValue{BoolValue: c.enableCDS},
					},
				},
			},
		},
		ResourceNames: []string{c.serviceName},
		TypeUrl:       edsType,
	}
	return edsReq
}

func (c *client) makeADSCall() {
	cli := xdsdiscoverypb.NewAggregatedDiscoveryServiceClient(c.cc)
	for ; ; c.loseContact(false) {
		select {
		case <-c.ctx.Done():
			break
		}
		ctx, cancel := context.WithCancel(c.ctx)
		st, err := cli.StreamAggregatedResources(ctx, grpc.WaitForReady(true))
		if err != nil {
			grpclog.Infof("xds: failed to initial ADS streaming RPC due to %v", err)
			continue
		}
		if c.enableCDS {
			if err := st.Send(c.newCDSRequest()); err != nil {
				// current stream is broken, start a new one.
				continue
			}
		}
		if err := st.Send(c.newEDSRequest()); err != nil {
			// current stream is broken, start a new one.
			continue
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
				cancel()
				break
			}
			switch resp.GetTypeUrl() {
			case cdsType:
				cdsResp := &xdspb.Cluster{}
				if err := proto.Unmarshal(resources[0].GetValue(), cdsResp); err != nil {
					cancel()
					break
				}
				if err := c.newADS(cdsResp); err != nil {
					cancel()
					break
				}
			case edsType:
				edsResp := &xdspb.ClusterLoadAssignment{}
				if err := proto.Unmarshal(resources[0].GetValue(), edsResp); err != nil {
					cancel()
					break
				}
				if err := c.newADS(edsResp); err != nil {
					cancel()
					break
				}
			default:
				grpclog.Warningf("xds: received response type not expected, type: %T, %v", resp, resp)
				cancel()
			}
		}
	}
}

func newXDSClient(balancerName string, serviceName string, enableCDS bool, opts balancer.BuildOptions, newADS func(interface{}) error, loseContact func(bool)) *client {
	c := &client{
		balancerName: balancerName,
		serviceName:  serviceName,
		enableCDS:    enableCDS,
		opts:         opts,
		newADS:       newADS,
		loseContact:  loseContact,
	}

	c.ctx, c.cancel = context.WithCancel(context.Background())

	var err error
	if opts.CredsBundle != nil {
		c.xdsClientConnCreds, err = opts.CredsBundle.NewWithMode(internal.CredsBundleModeBalancer)
		if err != nil {
			grpclog.Warningf("xdsClient: client connection creds NewWithMode failed: %v", err)
		}
		c.xdsBackendCreds, err = opts.CredsBundle.NewWithMode(internal.CredsBundleModeBackendFromBalancer)
		if err != nil {
			grpclog.Warningf("xdsClient: backend creds NewWithMode failed: %v", err)
		}
	}

	return c
}
