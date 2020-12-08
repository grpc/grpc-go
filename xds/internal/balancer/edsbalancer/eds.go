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

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/serviceconfig"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

const edsName = "eds_experimental"

// xdsClientInterface contains only the xds_client methods needed by EDS
// balancer. It's defined so we can override xdsclient.New function in tests.
type xdsClientInterface interface {
	WatchEndpoints(clusterName string, edsCb func(xdsclient.EndpointsUpdate, error)) (cancel func())
	ReportLoad(server string) (loadStore *load.Store, cancel func())
	Close()
}

var (
	newEDSBalancer = func(cc balancer.ClientConn, enqueueState func(priorityType, balancer.State), lw load.PerClusterReporter, logger *grpclog.PrefixLogger) edsBalancerImplInterface {
		return newEDSBalancerImpl(cc, enqueueState, lw, logger)
	}
	newXDSClient = func() (xdsClientInterface, error) { return xdsclient.New() }
)

func init() {
	balancer.Register(&edsBalancerBuilder{})
}

type edsBalancerBuilder struct{}

// Build helps implement the balancer.Builder interface.
func (b *edsBalancerBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	x := &edsBalancer{
		cc:                cc,
		closed:            grpcsync.NewEvent(),
		grpcUpdate:        make(chan interface{}),
		xdsClientUpdate:   make(chan *edsUpdate),
		childPolicyUpdate: buffer.NewUnbounded(),
		lsw:               &loadStoreWrapper{},
		config:            &EDSConfig{},
	}
	x.logger = prefixLogger(x)

	client, err := newXDSClient()
	if err != nil {
		x.logger.Errorf("xds: failed to create xds-client: %v", err)
		return nil
	}

	x.xdsClient = client
	x.edsImpl = newEDSBalancer(x.cc, x.enqueueChildBalancerState, x.lsw, x.logger)
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
	// updateServiceRequestsCounter updates the service requests counter to the
	// one for the given service name.
	updateServiceRequestsCounter(serviceName string)
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
	logger *grpclog.PrefixLogger

	// edsBalancer continuously monitors the channels below, and will handle
	// events from them in sync.
	grpcUpdate        chan interface{}
	xdsClientUpdate   chan *edsUpdate
	childPolicyUpdate *buffer.Unbounded

	xdsClient xdsClientInterface
	lsw       *loadStoreWrapper
	config    *EDSConfig // may change when passed a different service config
	edsImpl   edsBalancerImplInterface

	// edsServiceName is the edsServiceName currently being watched, not
	// necessary the edsServiceName from service config.
	edsServiceName       string
	cancelEndpointsWatch func()
	loadReportServer     *string // LRS is disabled if loadReporterServer is nil.
	cancelLoadReport     func()
}

// run gets executed in a goroutine once edsBalancer is created. It monitors
// updates from grpc, xdsClient and load balancer. It synchronizes the
// operations that happen inside edsBalancer. It exits when edsBalancer is
// closed.
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
		case <-x.closed.Done():
			x.cancelWatch()
			x.xdsClient.Close()
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
	x.logger.Warningf("Received error: %v", err)
	if xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
		if fromParent {
			// This is an error from the parent ClientConn (can be the parent
			// CDS balancer), and is a resource-not-found error. This means the
			// resource (can be either LDS or CDS) was removed. Stop the EDS
			// watch.
			x.cancelWatch()
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

		if err := x.handleServiceConfigUpdate(cfg); err != nil {
			x.logger.Warningf("failed to update xDS client: %v", err)
		}

		x.edsImpl.updateServiceRequestsCounter(cfg.EDSServiceName)

		// We will update the edsImpl with the new child policy, if we got a
		// different one.
		if !cmp.Equal(cfg.ChildPolicy, x.config.ChildPolicy, cmpopts.EquateEmpty()) {
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
		x.logger.Errorf("wrong update type: %T", update)
	}
}

// handleServiceConfigUpdate applies the service config update, watching a new
// EDS service name and restarting LRS stream, as required.
func (x *edsBalancer) handleServiceConfigUpdate(config *EDSConfig) error {
	// Restart EDS watch when the edsServiceName has changed.
	if x.edsServiceName != config.EDSServiceName {
		x.edsServiceName = config.EDSServiceName
		x.startEndpointsWatch()
		// TODO: this update for the LRS service name is too early. It should
		// only apply to the new EDS response. But this is applied to the RPCs
		// before the new EDS response. To fully fix this, the EDS balancer
		// needs to do a graceful switch to another EDS implementation.
		//
		// This is OK for now, because we don't actually expect edsServiceName
		// to change. Fix this (a bigger change) will happen later.
		x.lsw.updateServiceName(x.edsServiceName)
	}

	// Restart load reporting when the loadReportServer name has changed.
	if !equalStringPointers(x.loadReportServer, config.LrsLoadReportingServerName) {
		loadStore := x.startLoadReport(config.LrsLoadReportingServerName)
		x.lsw.updateLoadStore(loadStore)
	}

	return nil
}

// startEndpointsWatch starts the EDS watch.
//
// This usually means load report needs to be restarted, but this function does
// NOT do that. Caller needs to call startLoadReport separately.
func (x *edsBalancer) startEndpointsWatch() {
	if x.cancelEndpointsWatch != nil {
		x.cancelEndpointsWatch()
	}
	cancelEDSWatch := x.xdsClient.WatchEndpoints(x.edsServiceName, func(update xdsclient.EndpointsUpdate, err error) {
		x.logger.Infof("Watch update from xds-client %p, content: %+v", x.xdsClient, update)
		x.handleEDSUpdate(update, err)
	})
	x.logger.Infof("Watch started on resource name %v with xds-client %p", x.edsServiceName, x.xdsClient)
	x.cancelEndpointsWatch = func() {
		cancelEDSWatch()
		x.logger.Infof("Watch cancelled on resource name %v with xds-client %p", x.edsServiceName, x.xdsClient)
	}
}

func (x *edsBalancer) cancelWatch() {
	x.loadReportServer = nil
	if x.cancelLoadReport != nil {
		x.cancelLoadReport()
	}
	x.edsServiceName = ""
	if x.cancelEndpointsWatch != nil {
		x.cancelEndpointsWatch()
	}
}

// startLoadReport starts load reporting. If there's already a load reporting in
// progress, it cancels that.
//
// Caller can cal this when the loadReportServer name changes, but
// edsServiceName doesn't (so we only need to restart load reporting, not EDS
// watch).
func (x *edsBalancer) startLoadReport(loadReportServer *string) *load.Store {
	x.loadReportServer = loadReportServer
	if x.cancelLoadReport != nil {
		x.cancelLoadReport()
	}
	if loadReportServer == nil {
		return nil
	}
	ls, cancel := x.xdsClient.ReportLoad(*loadReportServer)
	x.cancelLoadReport = cancel
	return ls
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
	case <-x.closed.Done():
	}
}

func (x *edsBalancer) ResolverError(err error) {
	select {
	case x.grpcUpdate <- err:
	case <-x.closed.Done():
	}
}

func (x *edsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	select {
	case x.grpcUpdate <- &s:
	case <-x.closed.Done():
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
	case <-x.closed.Done():
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
	x.closed.Fire()
	x.logger.Infof("Shutdown")
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
