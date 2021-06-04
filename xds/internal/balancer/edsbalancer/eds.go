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
	"encoding/json"
	"fmt"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/balancer/loadstore"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

const edsName = "eds_experimental"

var (
	newEDSBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions, enqueueState func(priorityType, balancer.State), lw load.PerClusterReporter, logger *grpclog.PrefixLogger) edsBalancerImplInterface {
		return newEDSBalancerImpl(cc, opts, enqueueState, lw, logger)
	}
)

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	x := &edsBalancer{
		cc:                cc,
		closed:            grpcsync.NewEvent(),
		done:              grpcsync.NewEvent(),
		grpcUpdate:        make(chan interface{}),
		xdsClientUpdate:   make(chan *edsUpdate),
		childPolicyUpdate: buffer.NewUnbounded(),
		loadWrapper:       loadstore.NewWrapper(),
		config:            &EDSConfig{},
	}
	x.logger = prefixLogger(x)
	x.edsImpl = newEDSBalancer(x.cc, opts, x.enqueueChildBalancerState, x.loadWrapper, x.logger)
	x.logger.Infof("Created")
	go x.run()
	return x
}

func (bb) Name() string {
	return edsName
}

func (bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
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
	// handleEDSResponse passes the received EDS message from traffic director
	// to eds balancer.
	handleEDSResponse(edsResp xdsclient.EndpointsUpdate)
	// handleChildPolicy updates the eds balancer the intra-cluster load
	// balancing policy to use.
	handleChildPolicy(name string, config json.RawMessage)
	// handleSubConnStateChange handles state change for SubConn.
	handleSubConnStateChange(sc balancer.SubConn, state connectivity.State)
	// updateState handle a balancer state update from the priority.
	updateState(priority priorityType, s balancer.State)
	// updateServiceRequestsConfig updates the service requests counter to the
	// one for the given service name.
	updateServiceRequestsConfig(serviceName string, max *uint32)
	// updateClusterName updates the cluster name that will be attached to the
	// address attributes.
	updateClusterName(name string)
	// close closes the eds balancer.
	close()
}

// edsBalancer manages xdsClient and the actual EDS balancer implementation that
// does load balancing.
//
// It currently has only an edsBalancer. Later, we may add fallback.
type edsBalancer struct {
	cc     balancer.ClientConn
	closed *grpcsync.Event
	done   *grpcsync.Event
	logger *grpclog.PrefixLogger

	// edsBalancer continuously monitors the channels below, and will handle
	// events from them in sync.
	grpcUpdate        chan interface{}
	xdsClientUpdate   chan *edsUpdate
	childPolicyUpdate *buffer.Unbounded

	xdsClient   xdsclient.XDSClient
	loadWrapper *loadstore.Wrapper
	config      *EDSConfig // may change when passed a different service config
	edsImpl     edsBalancerImplInterface

	clusterName          string
	edsServiceName       string
	edsToWatch           string // this is edsServiceName if it's set, otherwise, it's clusterName.
	cancelEndpointsWatch func()
	loadReportServer     *string // LRS is disabled if loadReporterServer is nil.
	cancelLoadReport     func()
}

// run gets executed in a goroutine once edsBalancer is created. It monitors
// updates from grpc, xdsClient and load balancer. It synchronizes the
// operations that happen inside edsBalancer. It exits when edsBalancer is
// closed.
func (b *edsBalancer) run() {
	for {
		select {
		case update := <-b.grpcUpdate:
			b.handleGRPCUpdate(update)
		case update := <-b.xdsClientUpdate:
			b.handleXDSClientUpdate(update)
		case update := <-b.childPolicyUpdate.Get():
			b.childPolicyUpdate.Load()
			u := update.(*balancerStateWithPriority)
			b.edsImpl.updateState(u.priority, u.s)
		case <-b.closed.Done():
			b.cancelWatch()
			b.edsImpl.close()
			b.logger.Infof("Shutdown")
			b.done.Fire()
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
func (b *edsBalancer) handleErrorFromUpdate(err error, fromParent bool) {
	b.logger.Warningf("Received error: %v", err)
	if xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
		if fromParent {
			// This is an error from the parent ClientConn (can be the parent
			// CDS balancer), and is a resource-not-found error. This means the
			// resource (can be either LDS or CDS) was removed. Stop the EDS
			// watch.
			b.cancelWatch()
		}
		b.edsImpl.handleEDSResponse(xdsclient.EndpointsUpdate{})
	}
}

func (b *edsBalancer) handleGRPCUpdate(update interface{}) {
	switch u := update.(type) {
	case *subConnStateUpdate:
		b.edsImpl.handleSubConnStateChange(u.sc, u.state.ConnectivityState)
	case *balancer.ClientConnState:
		b.logger.Infof("Received update from resolver, balancer config: %+v", pretty.ToJSON(u.BalancerConfig))
		cfg, _ := u.BalancerConfig.(*EDSConfig)
		if cfg == nil {
			// service config parsing failed. should never happen.
			return
		}

		if err := b.handleServiceConfigUpdate(cfg); err != nil {
			b.logger.Warningf("failed to update xDS client: %v", err)
		}

		b.edsImpl.updateServiceRequestsConfig(cfg.ClusterName, cfg.MaxConcurrentRequests)

		// We will update the edsImpl with the new child policy, if we got a
		// different one.
		if !cmp.Equal(cfg.ChildPolicy, b.config.ChildPolicy, cmpopts.EquateEmpty()) {
			if cfg.ChildPolicy != nil {
				b.edsImpl.handleChildPolicy(cfg.ChildPolicy.Name, cfg.ChildPolicy.Config)
			} else {
				b.edsImpl.handleChildPolicy(roundrobin.Name, nil)
			}
		}
		b.config = cfg
	case error:
		b.handleErrorFromUpdate(u, true)
	default:
		// unreachable path
		b.logger.Errorf("wrong update type: %T", update)
	}
}

// handleServiceConfigUpdate applies the service config update, watching a new
// EDS service name and restarting LRS stream, as required.
func (b *edsBalancer) handleServiceConfigUpdate(config *EDSConfig) error {
	var updateLoadClusterAndService bool
	if b.clusterName != config.ClusterName {
		updateLoadClusterAndService = true
		b.clusterName = config.ClusterName
		b.edsImpl.updateClusterName(b.clusterName)
	}
	if b.edsServiceName != config.EDSServiceName {
		updateLoadClusterAndService = true
		b.edsServiceName = config.EDSServiceName
	}

	// If EDSServiceName is set, use it to watch EDS. Otherwise, use the cluster
	// name.
	newEDSToWatch := config.EDSServiceName
	if newEDSToWatch == "" {
		newEDSToWatch = config.ClusterName
	}
	var restartEDSWatch bool
	if b.edsToWatch != newEDSToWatch {
		restartEDSWatch = true
		b.edsToWatch = newEDSToWatch
	}

	// Restart EDS watch when the eds name has changed.
	if restartEDSWatch {
		b.startEndpointsWatch()
	}

	if updateLoadClusterAndService {
		// TODO: this update for the LRS service name is too early. It should
		// only apply to the new EDS response. But this is applied to the RPCs
		// before the new EDS response. To fully fix this, the EDS balancer
		// needs to do a graceful switch to another EDS implementation.
		//
		// This is OK for now, because we don't actually expect edsServiceName
		// to change. Fix this (a bigger change) will happen later.
		b.loadWrapper.UpdateClusterAndService(b.clusterName, b.edsServiceName)
	}

	// Restart load reporting when the loadReportServer name has changed.
	if !equalStringPointers(b.loadReportServer, config.LrsLoadReportingServerName) {
		loadStore := b.startLoadReport(config.LrsLoadReportingServerName)
		b.loadWrapper.UpdateLoadStore(loadStore)
	}

	return nil
}

// startEndpointsWatch starts the EDS watch.
//
// This usually means load report needs to be restarted, but this function does
// NOT do that. Caller needs to call startLoadReport separately.
func (b *edsBalancer) startEndpointsWatch() {
	if b.cancelEndpointsWatch != nil {
		b.cancelEndpointsWatch()
	}
	edsToWatch := b.edsToWatch
	cancelEDSWatch := b.xdsClient.WatchEndpoints(edsToWatch, func(update xdsclient.EndpointsUpdate, err error) {
		b.logger.Infof("Watch update from xds-client %p, content: %+v", b.xdsClient, pretty.ToJSON(update))
		b.handleEDSUpdate(update, err)
	})
	b.logger.Infof("Watch started on resource name %v with xds-client %p", edsToWatch, b.xdsClient)
	b.cancelEndpointsWatch = func() {
		cancelEDSWatch()
		b.logger.Infof("Watch cancelled on resource name %v with xds-client %p", edsToWatch, b.xdsClient)
	}
}

func (b *edsBalancer) cancelWatch() {
	b.loadReportServer = nil
	if b.cancelLoadReport != nil {
		b.cancelLoadReport()
		b.cancelLoadReport = nil
	}
	if b.cancelEndpointsWatch != nil {
		b.edsToWatch = ""
		b.cancelEndpointsWatch()
		b.cancelEndpointsWatch = nil
	}
}

// startLoadReport starts load reporting. If there's already a load reporting in
// progress, it cancels that.
//
// Caller can cal this when the loadReportServer name changes, but
// edsServiceName doesn't (so we only need to restart load reporting, not EDS
// watch).
func (b *edsBalancer) startLoadReport(loadReportServer *string) *load.Store {
	b.loadReportServer = loadReportServer
	if b.cancelLoadReport != nil {
		b.cancelLoadReport()
		b.cancelLoadReport = nil
	}
	if loadReportServer == nil {
		return nil
	}
	ls, cancel := b.xdsClient.ReportLoad(*loadReportServer)
	b.cancelLoadReport = cancel
	return ls
}

func (b *edsBalancer) handleXDSClientUpdate(update *edsUpdate) {
	if err := update.err; err != nil {
		b.handleErrorFromUpdate(err, false)
		return
	}
	b.edsImpl.handleEDSResponse(update.resp)
}

type subConnStateUpdate struct {
	sc    balancer.SubConn
	state balancer.SubConnState
}

func (b *edsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	update := &subConnStateUpdate{
		sc:    sc,
		state: state,
	}
	select {
	case b.grpcUpdate <- update:
	case <-b.closed.Done():
	}
}

func (b *edsBalancer) ResolverError(err error) {
	select {
	case b.grpcUpdate <- err:
	case <-b.closed.Done():
	}
}

func (b *edsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	if b.xdsClient == nil {
		c := xdsclient.FromResolverState(s.ResolverState)
		if c == nil {
			return balancer.ErrBadResolverState
		}
		b.xdsClient = c
	}

	select {
	case b.grpcUpdate <- &s:
	case <-b.closed.Done():
	}
	return nil
}

type edsUpdate struct {
	resp xdsclient.EndpointsUpdate
	err  error
}

func (b *edsBalancer) handleEDSUpdate(resp xdsclient.EndpointsUpdate, err error) {
	select {
	case b.xdsClientUpdate <- &edsUpdate{resp: resp, err: err}:
	case <-b.closed.Done():
	}
}

type balancerStateWithPriority struct {
	priority priorityType
	s        balancer.State
}

func (b *edsBalancer) enqueueChildBalancerState(p priorityType, s balancer.State) {
	b.childPolicyUpdate.Put(&balancerStateWithPriority{
		priority: p,
		s:        s,
	})
}

func (b *edsBalancer) Close() {
	b.closed.Fire()
	<-b.done.Done()
}

// equalStringPointers returns true if
// - a and b are both nil OR
// - *a == *b (and a and b are both non-nil)
func equalStringPointers(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
