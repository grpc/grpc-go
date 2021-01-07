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

// Package lrs implements load reporting balancer for xds.
package lrs

import (
	"encoding/json"
	"fmt"
	"sync"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/serviceconfig"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

func init() {
	balancer.Register(&lrsBB{})
}

var newXDSClient = func() (xdsClientInterface, error) { return xdsclient.New() }

const lrsBalancerName = "lrs_experimental"

type lrsBB struct{}

func (l *lrsBB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &lrsBalancer{
		cc:        cc,
		buildOpts: opts,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")

	client, err := newXDSClient()
	if err != nil {
		b.logger.Errorf("failed to create xds-client: %v", err)
		return nil
	}
	b.client = newXDSClientWrapper(client)

	return b
}

func (l *lrsBB) Name() string {
	return lrsBalancerName
}

func (l *lrsBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

type lrsBalancer struct {
	cc        balancer.ClientConn
	buildOpts balancer.BuildOptions

	logger *grpclog.PrefixLogger
	client *xdsClientWrapper

	config *lbConfig
	lb     balancer.Balancer // The sub balancer.
}

func (b *lrsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}

	// Update load reporting config or xds client. This needs to be done before
	// updating the child policy because we need the loadStore from the updated
	// client to be passed to the ccWrapper.
	if err := b.client.update(newConfig); err != nil {
		return err
	}

	// If child policy is a different type, recreate the sub-balancer.
	if b.config == nil || b.config.ChildPolicy.Name != newConfig.ChildPolicy.Name {
		bb := balancer.Get(newConfig.ChildPolicy.Name)
		if bb == nil {
			return fmt.Errorf("balancer %q not registered", newConfig.ChildPolicy.Name)
		}
		if b.lb != nil {
			b.lb.Close()
		}
		lidJSON, err := newConfig.Locality.ToString()
		if err != nil {
			return fmt.Errorf("failed to marshal LocalityID: %#v", newConfig.Locality)
		}
		ccWrapper := newCCWrapper(b.cc, b.client.loadStore(), lidJSON)
		b.lb = bb.Build(ccWrapper, b.buildOpts)
	}
	b.config = newConfig

	// Addresses and sub-balancer config are sent to sub-balancer.
	return b.lb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  s.ResolverState,
		BalancerConfig: b.config.ChildPolicy.Config,
	})
}

func (b *lrsBalancer) ResolverError(err error) {
	if b.lb != nil {
		b.lb.ResolverError(err)
	}
}

func (b *lrsBalancer) UpdateSubConnState(sc balancer.SubConn, s balancer.SubConnState) {
	if b.lb != nil {
		b.lb.UpdateSubConnState(sc, s)
	}
}

func (b *lrsBalancer) Close() {
	if b.lb != nil {
		b.lb.Close()
		b.lb = nil
	}
	b.client.close()
}

type ccWrapper struct {
	balancer.ClientConn
	loadStore      load.PerClusterReporter
	localityIDJSON string
}

func newCCWrapper(cc balancer.ClientConn, loadStore load.PerClusterReporter, localityIDJSON string) *ccWrapper {
	return &ccWrapper{
		ClientConn:     cc,
		loadStore:      loadStore,
		localityIDJSON: localityIDJSON,
	}
}

func (ccw *ccWrapper) UpdateState(s balancer.State) {
	s.Picker = newLoadReportPicker(s.Picker, ccw.localityIDJSON, ccw.loadStore)
	ccw.ClientConn.UpdateState(s)
}

// xdsClientInterface contains only the xds_client methods needed by LRS
// balancer. It's defined so we can override xdsclient in tests.
type xdsClientInterface interface {
	ReportLoad(server string) (*load.Store, func())
	Close()
}

type loadStoreWrapper struct {
	mu         sync.RWMutex
	cluster    string
	edsService string
	// Both store and perCluster will be nil if load reporting is disabled (EDS
	// response doesn't have LRS server name). Note that methods on Store and
	// perCluster all handle nil, so there's no need to check nil before calling
	// them.
	store      *load.Store
	perCluster load.PerClusterReporter
}

func (lsw *loadStoreWrapper) updateClusterAndService(cluster, edsService string) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()
	if cluster == lsw.cluster && edsService == lsw.edsService {
		return
	}
	lsw.cluster = cluster
	lsw.edsService = edsService
	lsw.perCluster = lsw.store.PerCluster(lsw.cluster, lsw.edsService)
}

func (lsw *loadStoreWrapper) updateLoadStore(store *load.Store) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()
	if store == lsw.store {
		return
	}
	lsw.store = store
	lsw.perCluster = nil
	lsw.perCluster = lsw.store.PerCluster(lsw.cluster, lsw.edsService)
}

func (lsw *loadStoreWrapper) CallStarted(locality string) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	if lsw.perCluster != nil {
		lsw.perCluster.CallStarted(locality)
	}
}

func (lsw *loadStoreWrapper) CallFinished(locality string, err error) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	if lsw.perCluster != nil {
		lsw.perCluster.CallFinished(locality, err)
	}
}

func (lsw *loadStoreWrapper) CallServerLoad(locality, name string, val float64) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	if lsw.perCluster != nil {
		lsw.perCluster.CallServerLoad(locality, name, val)
	}
}

func (lsw *loadStoreWrapper) CallDropped(category string) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	if lsw.perCluster != nil {
		lsw.perCluster.CallDropped(category)
	}
}

type xdsClientWrapper struct {
	c                xdsClientInterface
	cancelLoadReport func()
	clusterName      string
	edsServiceName   string
	lrsServerName    string
	// loadWrapper is a wrapper with loadOriginal, with clusterName and
	// edsServiceName. It's used children to report loads.
	loadWrapper *loadStoreWrapper
}

func newXDSClientWrapper(c xdsClientInterface) *xdsClientWrapper {
	return &xdsClientWrapper{
		c:           c,
		loadWrapper: &loadStoreWrapper{},
	}
}

// update checks the config and xdsclient, and decides whether it needs to
// restart the load reporting stream.
func (w *xdsClientWrapper) update(newConfig *lbConfig) error {
	var (
		restartLoadReport           bool
		updateLoadClusterAndService bool
	)

	// ClusterName is different, restart. ClusterName is from ClusterName and
	// EdsServiceName.
	if w.clusterName != newConfig.ClusterName {
		updateLoadClusterAndService = true
		w.clusterName = newConfig.ClusterName
	}
	if w.edsServiceName != newConfig.EdsServiceName {
		updateLoadClusterAndService = true
		w.edsServiceName = newConfig.EdsServiceName
	}

	if updateLoadClusterAndService {
		// This updates the clusterName and serviceName that will reported for the
		// loads. The update here is too early, the perfect timing is when the
		// picker is updated with the new connection. But from this balancer's point
		// of view, it's impossible to tell.
		//
		// On the other hand, this will almost never happen. Each LRS policy
		// shouldn't get updated config. The parent should do a graceful switch when
		// the clusterName or serviceName is changed.
		w.loadWrapper.updateClusterAndService(w.clusterName, w.edsServiceName)
	}

	if w.lrsServerName != newConfig.LrsLoadReportingServerName {
		// LrsLoadReportingServerName is different, load should be report to a
		// different server, restart.
		restartLoadReport = true
		w.lrsServerName = newConfig.LrsLoadReportingServerName
	}

	if restartLoadReport {
		if w.cancelLoadReport != nil {
			w.cancelLoadReport()
			w.cancelLoadReport = nil
		}
		var loadStore *load.Store
		if w.c != nil {
			loadStore, w.cancelLoadReport = w.c.ReportLoad(w.lrsServerName)
		}
		w.loadWrapper.updateLoadStore(loadStore)
	}

	return nil
}

func (w *xdsClientWrapper) loadStore() load.PerClusterReporter {
	return w.loadWrapper
}

func (w *xdsClientWrapper) close() {
	if w.cancelLoadReport != nil {
		w.cancelLoadReport()
		w.cancelLoadReport = nil
	}
	w.c.Close()
}
