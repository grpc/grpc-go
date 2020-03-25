/*
 *
 * Copyright 2020 gRPC authors.
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

package rls

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/resolver"
)

var _ balancer.Balancer = (*rlsBalancer)(nil)
var _ balancer.V2Balancer = (*rlsBalancer)(nil)

// rlsBalancer implements the RLS LB policy.
type rlsBalancer struct {
	ctx    context.Context
	cancel context.CancelFunc
	cc     balancer.ClientConn
	opts   balancer.BuildOptions

	// Contains the most recently received service config.
	lbCfg *lbConfig
	rlsC  *rlsClient

	ccUpdateCh chan *balancer.ClientConnState
}

func (lb *rlsBalancer) run() {
	for {
		select {
		case u := <-lb.ccUpdateCh:
			lb.handleClientConnUpdate(u)
		case <-lb.ctx.Done():
			return
		}
	}
}

func (lb *rlsBalancer) handleClientConnUpdate(ccs *balancer.ClientConnState) {
	grpclog.Infof("rls: service config: %+v", ccs.BalancerConfig)
	newCfg := ccs.BalancerConfig.(*lbConfig)
	oldCfg := lb.lbCfg
	if oldCfg == nil {
		// This is the first time we are receiving a service config.
		oldCfg = &lbConfig{}
	}

	if oldCfg.Equal(newCfg) {
		grpclog.Info("rls: new service config matches existing config")
		return
	}

	if newCfg.lookupService != oldCfg.lookupService || newCfg.lookupServiceTimeout != oldCfg.lookupServiceTimeout {
		lb.updateControlChannel(newCfg.lookupService, newCfg.lookupServiceTimeout)
	}

	lb.lbCfg = newCfg
}

func (lb *rlsBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	select {
	case lb.ccUpdateCh <- &ccs:
	case <-lb.ctx.Done():
	}
	return nil
}

func (lb *rlsBalancer) ResolverError(error) {
	// ResolverError is called by gRPC when the name resolver reports an error.
	// TODO(easwars): How do we handle this?
}

func (lb *rlsBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {
	grpclog.Error("rlsbalancer.UpdateSubConnState is not yet implemented")
}

func (lb *rlsBalancer) Close() {
	lb.cancel()
	if lb.rlsC != nil {
		lb.rlsC.close()
	}
}

func (lb *rlsBalancer) HandleSubConnStateChange(_ balancer.SubConn, _ connectivity.State) {
	grpclog.Errorf("UpdateSubConnState should be called instead of HandleSubConnStateChange")
}

func (lb *rlsBalancer) HandleResolvedAddrs(_ []resolver.Address, _ error) {
	grpclog.Errorf("UpdateClientConnState should be called instead of HandleResolvedAddrs")
}

// updateControlChannel updates the RLS client if required.
func (lb *rlsBalancer) updateControlChannel(service string, timeout time.Duration) {
	var dopts []grpc.DialOption
	if dialer := lb.opts.Dialer; dialer != nil {
		dopts = append(dopts, grpc.WithContextDialer(dialer))
	}
	dopts = append(dopts, dialCreds(lb.opts))

	cc, err := grpc.Dial(service, dopts...)
	if err != nil {
		grpclog.Errorf("rls: dialRLS(%s, %v): %v", service, lb.opts, err)
		// An error from a non-blocking dial indicates something serious. We
		// should continue to use the old control channel if one exists, and
		// return so that the rest of the config updates can be processes.
		return
	}
	newClient := newRLSClient(cc, lb.opts.Target.Endpoint, timeout)
	if lb.rlsC != nil {
		lb.rlsC.close()
	}
	lb.rlsC = newClient
}

func dialCreds(opts balancer.BuildOptions) grpc.DialOption {
	// The control channel should use the same authority as that of the parent
	// channel. This ensures that the identify of the RLS server and that of the
	// backend is the same, so if the RLS config is injected by an attacker, it
	// cannot cause leakage of private information contained in headers set by
	// the application.
	server := opts.Target.Authority
	switch {
	case opts.DialCreds != nil:
		if err := opts.DialCreds.OverrideServerName(server); err != nil {
			grpclog.Warningf("rls: OverrideServerName(%s) = (%v), using Insecure", server, err)
			return grpc.WithInsecure()
		}
		return grpc.WithTransportCredentials(opts.DialCreds)
	case opts.CredsBundle != nil:
		return grpc.WithTransportCredentials(opts.CredsBundle.TransportCredentials())
	default:
		grpclog.Warning("rls: no credentials available, using Insecure")
		return grpc.WithInsecure()
	}
}
