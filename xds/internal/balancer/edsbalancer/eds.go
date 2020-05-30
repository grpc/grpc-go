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

// Package edsbalancer contains EDS balancer implementation.
package edsbalancer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/lrs"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	edsName = "eds_experimental"
)

var (
	newEDSBalancer = func(cc balancer.ClientConn, enqueueState func(priorityType, balancer.State), loadStore lrs.Store, logger *grpclog.PrefixLogger) edsBalancerImplInterface {
		return newEDSBalancerImpl(cc, enqueueState, loadStore, logger)
	}
)

func init() {
	balancer.Register(&edsBalancerBuilder{})
}

type edsBalancerBuilder struct{}

// Build helps implement the balancer.Builder interface.
func (b *edsBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	ctx, cancel := context.WithCancel(context.Background())
	x := &edsBalancer{
		ctx:               ctx,
		cancel:            cancel,
		cc:                cc,
		buildOpts:         opts,
		grpcUpdate:        make(chan interface{}),
		xdsClientUpdate:   make(chan *edsUpdate),
		childPolicyUpdate: buffer.NewUnbounded(),
	}
	loadStore := lrs.NewStore()
	x.logger = grpclog.NewPrefixLogger(loggingPrefix(x))
	x.edsImpl = newEDSBalancer(x.cc, x.enqueueChildBalancerState, loadStore, x.logger)
	x.client = newXDSClientWrapper(x.handleEDSUpdate, x.buildOpts, loadStore, x.logger)
	x.logger.Infof("Created")
	go x.run()
	return x
}

func (b *edsBalancerBuilder) Name() string {
	return edsName
}

func (b *edsBalancerBuilder) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg EDSConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("unable to unmarshal balancer config %s into EDSConfig, error: %v", string(c), err)
	}
	return &cfg, nil
}

// edsBalancerImplInterface defines the interface that edsBalancerImpl must
// implement to communicate with edsBalancer.
//
// It's implemented by the real eds balancer and a fake testing eds balancer.
type edsBalancerImplInterface interface {
	// handleEDSResponse passes the received EDS message from traffic director to eds balancer.
	handleEDSResponse(edsResp xdsclient.EndpointsUpdate)
	// handleChildPolicy updates the eds balancer the intra-cluster load balancing policy to use.
	handleChildPolicy(name string, config json.RawMessage)
	// handleSubConnStateChange handles state change for SubConn.
	handleSubConnStateChange(sc balancer.SubConn, state connectivity.State)
	// updateState handle a balancer state update from the priority.
	updateState(priority priorityType, s balancer.State)
	// close closes the eds balancer.
	close()
}

// edsBalancer manages xdsClient and the actual EDS balancer implementation that
// does load balancing.
//
// It currently has only an edsBalancer. Later, we may add fallback.
type edsBalancer struct {
	cc        balancer.ClientConn // *xdsClientConn
	buildOpts balancer.BuildOptions
	ctx       context.Context
	cancel    context.CancelFunc

	logger *grpclog.PrefixLogger

	// edsBalancer continuously monitor the channels below, and will handle events from them in sync.
	grpcUpdate        chan interface{}
	xdsClientUpdate   chan *edsUpdate
	childPolicyUpdate *buffer.Unbounded

	client  *xdsclientWrapper // may change when passed a different service config
	config  *EDSConfig        // may change when passed a different service config
	edsImpl edsBalancerImplInterface
}

// run gets executed in a goroutine once edsBalancer is created. It monitors updates from grpc,
// xdsClient and load balancer. It synchronizes the operations that happen inside edsBalancer. It
// exits when edsBalancer is closed.
func (x *edsBalancer) run() {
	for {
		select {
		case update := <-x.grpcUpdate:
			x.handleGRPCUpdate(update)
		case update := <-x.xdsClientUpdate:
			x.handleXDSClientUpdate(update)
		case update := <-x.childPolicyUpdate.Get():
			x.childPolicyUpdate.Load()
			u := update.(*balancerStateWithPriority)
			x.edsImpl.updateState(u.priority, u.s)
		case <-x.ctx.Done():
			x.client.close()
			x.edsImpl.close()
			return
		}
	}
}

// handleErrorFromUpdate handles both the error from parent ClientConn (from CDS
// balancer) and the error from xds client (from the watcher). fromParent is
// true if error is from parent ClientConn.
//
// If the error is connection error, it should be handled for fallback purposes.
//
// If the error is resource-not-found:
// - If it's from CDS balancer (shows as a resolver error), it means LDS or CDS
// resources were removed. The EDS watch should be canceled.
// - If it's from xds client, it means EDS resource were removed. The EDS
// watcher should keep watching.
// In both cases, the sub-balancers will be closed, and the future picks will
// fail.
func (x *edsBalancer) handleErrorFromUpdate(err error, fromParent bool) {
	if xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
		if fromParent {
			// This is an error from the parent ClientConn (can be the parent
			// CDS balancer), and is a resource-not-found error. This means the
			// resource (can be either LDS or CDS) was removed. Stop the EDS
			// watch.
			x.client.cancelWatch()
		}
		x.edsImpl.handleEDSResponse(xdsclient.EndpointsUpdate{})
	}
}

func (x *edsBalancer) handleGRPCUpdate(update interface{}) {
	switch u := update.(type) {
	case *subConnStateUpdate:
		x.edsImpl.handleSubConnStateChange(u.sc, u.state.ConnectivityState)
	case *balancer.ClientConnState:
		x.logger.Infof("Receive update from resolver, balancer config: %+v", u.BalancerConfig)
		cfg, _ := u.BalancerConfig.(*EDSConfig)
		if cfg == nil {
			// service config parsing failed. should never happen.
			return
		}

		x.client.handleUpdate(cfg, u.ResolverState.Attributes)

		if x.config == nil {
			x.config = cfg
			return
		}

		// We will update the edsImpl with the new child policy, if we got a
		// different one.
		if !cmp.Equal(cfg.ChildPolicy, x.config.ChildPolicy) {
			if cfg.ChildPolicy != nil {
				x.edsImpl.handleChildPolicy(cfg.ChildPolicy.Name, cfg.ChildPolicy.Config)
			} else {
				x.edsImpl.handleChildPolicy(roundrobin.Name, nil)
			}
		}

		x.config = cfg
	case error:
		x.handleErrorFromUpdate(u, true)
	default:
		// unreachable path
		panic("wrong update type")
	}
}

func (x *edsBalancer) handleXDSClientUpdate(update *edsUpdate) {
	if err := update.err; err != nil {
		x.handleErrorFromUpdate(err, false)
		return
	}
	x.edsImpl.handleEDSResponse(update.resp)
}

type subConnStateUpdate struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func (x *edsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	update := &subConnStateUpdate{
		sc:    sc,
		state: state,
	}
	select {
	case x.grpcUpdate <- update:
	case <-x.ctx.Done():
	}
}

func (x *edsBalancer) ResolverError(err error) {
	select {
	case x.grpcUpdate <- err:
	case <-x.ctx.Done():
	}
}

func (x *edsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	select {
	case x.grpcUpdate <- &s:
	case <-x.ctx.Done():
	}
	return nil
}

type edsUpdate struct {
	resp xdsclient.EndpointsUpdate
	err  error
}

func (x *edsBalancer) handleEDSUpdate(resp xdsclient.EndpointsUpdate, err error) {
	select {
	case x.xdsClientUpdate <- &edsUpdate{resp: resp, err: err}:
	case <-x.ctx.Done():
	}
}

type balancerStateWithPriority struct {
	priority priorityType
	s        balancer.State
}

func (x *edsBalancer) enqueueChildBalancerState(p priorityType, s balancer.State) {
	x.childPolicyUpdate.Put(&balancerStateWithPriority{
		priority: p,
		s:        s,
	})
}

func (x *edsBalancer) Close() {
	x.cancel()
	x.logger.Infof("Shutdown")
}
