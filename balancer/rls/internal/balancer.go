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
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
)

var (
	_ balancer.Balancer = (*rlsBalancer)(nil)

	// For overriding in tests.
	newRLSClientFunc = newRLSClient
	logger           = grpclog.Component("rls")
)

// rlsBalancer implements the RLS LB policy.
type rlsBalancer struct {
	done *grpcsync.Event
	cc   balancer.ClientConn
	opts balancer.BuildOptions

	// Mutex protects all the state maintained by the LB policy.
	// TODO(easwars): Once we add the cache, we will also have another lock for
	// the cache alone.
	mu    sync.Mutex
	lbCfg *lbConfig        // Most recently received service config.
	rlsCC *grpc.ClientConn // ClientConn to the RLS server.
	rlsC  *rlsClient       // RLS client wrapper.

	ccUpdateCh chan *balancer.ClientConnState
}

// run is a long running goroutine which handles all the updates that the
// balancer wishes to handle. The appropriate updateHandler will push the update
// on to a channel that this goroutine will select on, thereby the handling of
// the update will happen asynchronously.
func (lb *rlsBalancer) run() {
	for {
		// TODO(easwars): Handle other updates like subConn state changes, RLS
		// responses from the server etc.
		select {
		case u := <-lb.ccUpdateCh:
			lb.handleClientConnUpdate(u)
		case <-lb.done.Done():
			return
		}
	}
}

// handleClientConnUpdate handles updates to the service config.
// If the RLS server name or the RLS RPC timeout changes, it updates the control
// channel accordingly.
// TODO(easwars): Handle updates to other fields in the service config.
func (lb *rlsBalancer) handleClientConnUpdate(ccs *balancer.ClientConnState) {
	logger.Infof("rls: service config: %+v", ccs.BalancerConfig)
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.done.HasFired() {
		logger.Warning("rls: received service config after balancer close")
		return
	}

	newCfg := ccs.BalancerConfig.(*lbConfig)
	if lb.lbCfg.Equal(newCfg) {
		logger.Info("rls: new service config matches existing config")
		return
	}

	lb.updateControlChannel(newCfg)
	lb.lbCfg = newCfg
}

// UpdateClientConnState pushes the received ClientConnState update on the
// update channel which will be processed asynchronously by the run goroutine.
// Implements balancer.Balancer interface.
func (lb *rlsBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	select {
	case lb.ccUpdateCh <- &ccs:
	case <-lb.done.Done():
	}
	return nil
}

// ResolverErr implements balancer.Balancer interface.
func (lb *rlsBalancer) ResolverError(error) {
	// ResolverError is called by gRPC when the name resolver reports an error.
	// TODO(easwars): How do we handle this?
	logger.Fatal("rls: ResolverError is not yet unimplemented")
}

// UpdateSubConnState implements balancer.Balancer interface.
func (lb *rlsBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {
	logger.Fatal("rls: UpdateSubConnState is not yet implemented")
}

// Cleans up the resources allocated by the LB policy including the clientConn
// to the RLS server.
// Implements balancer.Balancer.
func (lb *rlsBalancer) Close() {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.done.Fire()
	if lb.rlsCC != nil {
		lb.rlsCC.Close()
	}
}

// updateControlChannel updates the RLS client if required.
// Caller must hold lb.mu.
func (lb *rlsBalancer) updateControlChannel(newCfg *lbConfig) {
	oldCfg := lb.lbCfg
	if newCfg.lookupService == oldCfg.lookupService && newCfg.lookupServiceTimeout == oldCfg.lookupServiceTimeout {
		return
	}

	// Use RPC timeout from new config, if different from existing one.
	timeout := oldCfg.lookupServiceTimeout
	if timeout != newCfg.lookupServiceTimeout {
		timeout = newCfg.lookupServiceTimeout
	}

	if newCfg.lookupService == oldCfg.lookupService {
		// This is the case where only the timeout has changed. We will continue
		// to use the existing clientConn. but will create a new rlsClient with
		// the new timeout.
		lb.rlsC = newRLSClientFunc(lb.rlsCC, lb.opts.Target.Endpoint, timeout)
		return
	}

	// This is the case where the RLS server name has changed. We need to create
	// a new clientConn and close the old one.
	var dopts []grpc.DialOption
	if dialer := lb.opts.Dialer; dialer != nil {
		dopts = append(dopts, grpc.WithContextDialer(dialer))
	}
	dopts = append(dopts, dialCreds(lb.opts))

	cc, err := grpc.Dial(newCfg.lookupService, dopts...)
	if err != nil {
		logger.Errorf("rls: dialRLS(%s, %v): %v", newCfg.lookupService, lb.opts, err)
		// An error from a non-blocking dial indicates something serious. We
		// should continue to use the old control channel if one exists, and
		// return so that the rest of the config updates can be processes.
		return
	}
	if lb.rlsCC != nil {
		lb.rlsCC.Close()
	}
	lb.rlsCC = cc
	lb.rlsC = newRLSClientFunc(cc, lb.opts.Target.Endpoint, timeout)
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
			logger.Warningf("rls: OverrideServerName(%s) = (%v), using Insecure", server, err)
			return grpc.WithInsecure()
		}
		return grpc.WithTransportCredentials(opts.DialCreds)
	case opts.CredsBundle != nil:
		return grpc.WithTransportCredentials(opts.CredsBundle.TransportCredentials())
	default:
		logger.Warning("rls: no credentials available, using Insecure")
		return grpc.WithInsecure()
	}
}
