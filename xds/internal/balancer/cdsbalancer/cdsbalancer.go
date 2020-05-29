/*
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
 */

// Package cdsbalancer implements a balancer to handle CDS responses.
package cdsbalancer

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/edsbalancer"

	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	cdsName = "cds_experimental"
	edsName = "eds_experimental"
)

var (
	errBalancerClosed = errors.New("cdsBalancer is closed")

	// newEDSBalancer is a helper function to build a new edsBalancer and will be
	// overridden in unittests.
	newEDSBalancer = func(cc balancer.ClientConn, opts balancer.BuildOptions) (balancer.Balancer, error) {
		builder := balancer.Get(edsName)
		if builder == nil {
			return nil, fmt.Errorf("xds: no balancer builder with name %v", edsName)
		}
		// We directly pass the parent clientConn to the
		// underlying edsBalancer because the cdsBalancer does
		// not deal with subConns.
		return builder.Build(cc, opts), nil
	}
)

func init() {
	balancer.Register(cdsBB{})
}

// cdsBB (short for cdsBalancerBuilder) implements the balancer.Builder
// interface to help build a cdsBalancer.
// It also implements the balancer.ConfigParser interface to help parse the
// JSON service config, to be passed to the cdsBalancer.
type cdsBB struct{}

// Build creates a new CDS balancer with the ClientConn.
func (cdsBB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &cdsBalancer{
		cc:       cc,
		bOpts:    opts,
		updateCh: buffer.NewUnbounded(),
	}
	b.logger = grpclog.NewPrefixLogger(loggingPrefix(b))
	b.logger.Infof("Created")
	go b.run()
	return b
}

// Name returns the name of balancers built by this builder.
func (cdsBB) Name() string {
	return cdsName
}

// lbConfig represents the loadBalancingConfig section of the service config
// for the cdsBalancer.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	ClusterName string `json:"Cluster"`
}

// ParseConfig parses the JSON load balancer config provided into an
// internal form or returns an error if the config is invalid.
func (cdsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	var cfg lbConfig
	if err := json.Unmarshal(c, &cfg); err != nil {
		return nil, fmt.Errorf("xds: unable to unmarshal lbconfig: %s, error: %v", string(c), err)
	}
	return &cfg, nil
}

// xdsClientInterface contains methods from xdsClient.Client which are used by
// the cdsBalancer. This will be faked out in unittests.
type xdsClientInterface interface {
	WatchCluster(string, func(xdsclient.ClusterUpdate, error)) func()
	Close()
}

// ccUpdate wraps a clientConn update received from gRPC (pushed from the
// xdsResolver). A valid clusterName causes the cdsBalancer to register a CDS
// watcher with the xdsClient, while a non-nil error causes it to cancel the
// existing watch and propagate the error to the underlying edsBalancer.
type ccUpdate struct {
	client      xdsClientInterface
	clusterName string
	err         error
}

// scUpdate wraps a subConn update received from gRPC. This is directly passed
// on to the edsBalancer.
type scUpdate struct {
	subConn balancer.SubConn
	state   balancer.SubConnState
}

// watchUpdate wraps the information received from a registered CDS watcher. A
// non-nil error is propagated to the underlying edsBalancer. A valid update
// results in creating a new edsBalancer (if one doesn't already exist) and
// pushing the update to it.
type watchUpdate struct {
	cds xdsclient.ClusterUpdate
	err error
}

// closeUpdate is an empty struct used to notify the run() goroutine that a
// Close has been called on the balancer.
type closeUpdate struct{}

// cdsBalancer implements a CDS based LB policy. It instantiates an EDS based
// LB policy to further resolve the serviceName received from CDS, into
// localities and endpoints. Implements the balancer.Balancer interface which
// is exposed to gRPC and implements the balancer.ClientConn interface which is
// exposed to the edsBalancer.
type cdsBalancer struct {
	cc             balancer.ClientConn
	bOpts          balancer.BuildOptions
	updateCh       *buffer.Unbounded
	client         xdsClientInterface
	cancelWatch    func()
	edsLB          balancer.Balancer
	clusterToWatch string

	logger *grpclog.PrefixLogger

	// The only thing protected by this mutex is the closed boolean. This is
	// checked by all methods before acting on updates.
	mu     sync.Mutex
	closed bool
}

// run is a long-running goroutine which handles all updates from gRPC. All
// methods which are invoked directly by gRPC or xdsClient simply push an
// update onto a channel which is read and acted upon right here.
//
// 1. Good clientConn updates lead to registration of a CDS watch. Updates with
//    error lead to cancellation of existing watch and propagation of the same
//    error to the edsBalancer.
// 2. SubConn updates are passthrough and are simply handed over to the
//    underlying edsBalancer.
// 3. Watch API updates lead to clientConn updates being invoked on the
//    underlying edsBalancer.
// 4. Close results in cancellation of the CDS watch and closing of the
//    underlying edsBalancer and is the only way to exit this goroutine.
func (b *cdsBalancer) run() {
	for {
		u := <-b.updateCh.Get()
		b.updateCh.Load()
		switch update := u.(type) {
		case *ccUpdate:
			// We first handle errors, if any, and then proceed with handling
			// the update, only if the status quo has changed.
			if err := update.err; err != nil {
				b.handleErrorFromUpdate(err, true)
			}
			if b.client == update.client && b.clusterToWatch == update.clusterName {
				break
			}
			if update.client != nil {
				// Since the cdsBalancer doesn't own the xdsClient object, we
				// don't have to bother about closing the old client here, but
				// we still need to cancel the watch on the old client.
				if b.cancelWatch != nil {
					b.cancelWatch()
				}
				b.client = update.client
			}
			if update.clusterName != "" {
				cancelWatch := b.client.WatchCluster(update.clusterName, b.handleClusterUpdate)
				b.logger.Infof("Watch started on resource name %v with xds-client %p", update.clusterName, b.client)
				b.cancelWatch = func() {
					cancelWatch()
					b.logger.Infof("Watch cancelled on resource name %v with xds-client %p", update.clusterName, b.client)
				}
				b.clusterToWatch = update.clusterName
			}
		case *scUpdate:
			if b.edsLB == nil {
				b.logger.Errorf("xds: received scUpdate {%+v} with no edsBalancer", update)
				break
			}
			b.edsLB.UpdateSubConnState(update.subConn, update.state)
		case *watchUpdate:
			if err := update.err; err != nil {
				b.logger.Warningf("Watch error from xds-client %p: %v", b.client, err)
				b.handleErrorFromUpdate(err, false)
				break
			}

			b.logger.Infof("Watch update from xds-client %p, content: %+v", b.client, update.cds)
			// The first good update from the watch API leads to the
			// instantiation of an edsBalancer. Further updates/errors are
			// propagated to the existing edsBalancer.
			if b.edsLB == nil {
				var err error
				b.edsLB, err = newEDSBalancer(b.cc, b.bOpts)
				if b.edsLB == nil {
					b.logger.Errorf("Failed to create child policy of type %s, %v", edsName, err)
					break
				}
				b.logger.Infof("Created child policy %p of type %s", b.edsLB, edsName)
			}
			lbCfg := &edsbalancer.EDSConfig{EDSServiceName: update.cds.ServiceName}
			if update.cds.EnableLRS {
				// An empty string here indicates that the edsBalancer
				// should use the same xDS server for load reporting as
				// it does for EDS requests/responses.
				lbCfg.LrsLoadReportingServerName = new(string)

			}
			ccState := balancer.ClientConnState{
				ResolverState:  resolver.State{Attributes: attributes.New(xdsinternal.XDSClientID, b.client)},
				BalancerConfig: lbCfg,
			}
			if err := b.edsLB.UpdateClientConnState(ccState); err != nil {
				b.logger.Errorf("xds: edsBalancer.UpdateClientConnState(%+v) returned error: %v", ccState, err)
			}
		case *closeUpdate:
			if b.cancelWatch != nil {
				b.cancelWatch()
				b.cancelWatch = nil
			}
			if b.edsLB != nil {
				b.edsLB.Close()
				b.edsLB = nil
			}
			// This is the *ONLY* point of return from this function.
			b.logger.Infof("Shutdown")
			return
		}
	}
}

// handleErrorFromUpdate handles both the error from parent ClientConn (from
// resolver) and the error from xds client (from the watcher). fromParent is
// true if error is from parent ClientConn.
//
// If the error is connection error, it's passed down to the child policy.
// Nothing needs to be done in CDS (e.g. it doesn't go into fallback).
//
// If the error is resource-not-found:
// - If it's from resolver, it means LDS resources were removed. The CDS watch
// should be canceled.
// - If it's from xds client, it means CDS resource were removed. The CDS
// watcher should keep watching.
//
// In both cases, the error will be forwarded to EDS balancer. And if error is
// resource-not-found, the child EDS balancer will stop watching EDS.
func (b *cdsBalancer) handleErrorFromUpdate(err error, fromParent bool) {
	// TODO: connection errors will be sent to the eds balancers directly, and
	// also forwarded by the parent balancers/resolvers. So the eds balancer may
	// see the same error multiple times. We way want to only forward the error
	// to eds if it's not a connection error.
	//
	// This is not necessary today, because xds client never sends connection
	// errors.

	if fromParent && xdsclient.ErrType(err) == xdsclient.ErrorTypeResourceNotFound {
		if b.cancelWatch != nil {
			b.cancelWatch()
		}
	}
	if b.edsLB != nil {
		b.edsLB.ResolverError(err)
	} else {
		// If eds balancer was never created, fail the RPCs with
		// errors.
		b.cc.UpdateState(balancer.State{
			ConnectivityState: connectivity.TransientFailure,
			Picker:            base.NewErrPicker(err),
		})
	}
}

// handleClusterUpdate is the CDS watch API callback. It simply pushes the
// received information on to the update channel for run() to pick it up.
func (b *cdsBalancer) handleClusterUpdate(cu xdsclient.ClusterUpdate, err error) {
	if b.isClosed() {
		b.logger.Warningf("xds: received cluster update {%+v} after cdsBalancer was closed", cu)
		return
	}
	b.updateCh.Put(&watchUpdate{cds: cu, err: err})
}

// UpdateClientConnState receives the serviceConfig (which contains the
// clusterName to watch for in CDS) and the xdsClient object from the
// xdsResolver.
func (b *cdsBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
	if b.isClosed() {
		b.logger.Warningf("xds: received ClientConnState {%+v} after cdsBalancer was closed", state)
		return errBalancerClosed
	}

	b.logger.Infof("Receive update from resolver, balancer config: %+v", state.BalancerConfig)
	// The errors checked here should ideally never happen because the
	// ServiceConfig in this case is prepared by the xdsResolver and is not
	// something that is received on the wire.
	lbCfg, ok := state.BalancerConfig.(*lbConfig)
	if !ok {
		b.logger.Warningf("xds: unexpected LoadBalancingConfig type: %T", state.BalancerConfig)
		return balancer.ErrBadResolverState
	}
	if lbCfg.ClusterName == "" {
		b.logger.Warningf("xds: no clusterName found in LoadBalancingConfig: %+v", lbCfg)
		return balancer.ErrBadResolverState
	}
	client := state.ResolverState.Attributes.Value(xdsinternal.XDSClientID)
	if client == nil {
		b.logger.Warningf("xds: no xdsClient found in resolver state attributes")
		return balancer.ErrBadResolverState
	}
	newClient, ok := client.(xdsClientInterface)
	if !ok {
		b.logger.Warningf("xds: unexpected xdsClient type: %T", client)
		return balancer.ErrBadResolverState
	}
	b.updateCh.Put(&ccUpdate{client: newClient, clusterName: lbCfg.ClusterName})
	return nil
}

// ResolverError handles errors reported by the xdsResolver.
func (b *cdsBalancer) ResolverError(err error) {
	if b.isClosed() {
		b.logger.Warningf("xds: received resolver error {%v} after cdsBalancer was closed", err)
		return
	}

	b.updateCh.Put(&ccUpdate{err: err})
}

// UpdateSubConnState handles subConn updates from gRPC.
func (b *cdsBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	if b.isClosed() {
		b.logger.Warningf("xds: received subConn update {%v, %v} after cdsBalancer was closed", sc, state)
		return
	}
	b.updateCh.Put(&scUpdate{subConn: sc, state: state})
}

// Close closes the cdsBalancer and the underlying edsBalancer.
func (b *cdsBalancer) Close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.updateCh.Put(&closeUpdate{})
}

func (b *cdsBalancer) isClosed() bool {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	return closed
}
