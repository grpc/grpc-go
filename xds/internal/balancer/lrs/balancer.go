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

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal"
	xdsinternal "google.golang.org/grpc/xds/internal"
)

func init() {
	balancer.Register(&lrsBB{})
}

const lrsBalancerName = "lrs_experimental"

type lrsBB struct{}

func (l *lrsBB) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &lrsBalancer{
		cc:        cc,
		buildOpts: opts,
	}
	b.loadStore = NewStore()
	b.client = newXDSClientWrapper(b.loadStore)
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
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

	logger    *grpclog.PrefixLogger
	loadStore Store
	client    *xdsClientWrapper

	config *lbConfig
	lb     balancer.Balancer // The sub balancer.
}

func (b *lrsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
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
		b.lb = bb.Build(newCCWrapper(b.cc, b.loadStore, newConfig.Locality), b.buildOpts)
	}
	// Update load reporting config or xds client.
	b.client.update(newConfig, s.ResolverState.Attributes)
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
	loadStore  Store
	localityID *internal.LocalityID
}

func newCCWrapper(cc balancer.ClientConn, loadStore Store, localityID *internal.LocalityID) *ccWrapper {
	return &ccWrapper{
		ClientConn: cc,
		loadStore:  loadStore,
		localityID: localityID,
	}
}

func (ccw *ccWrapper) UpdateState(s balancer.State) {
	s.Picker = newLoadReportPicker(s.Picker, *ccw.localityID, ccw.loadStore)
	ccw.ClientConn.UpdateState(s)
}

// xdsClientInterface contains only the xds_client methods needed by LRS
// balancer. It's defined so we can override xdsclient in tests.
type xdsClientInterface interface {
	ReportLoad(server string, clusterName string, loadStore Store) (cancel func())
	Close()
}

type xdsClientWrapper struct {
	loadStore Store

	c                xdsClientInterface
	cancelLoadReport func()
	clusterName      string
	lrsServerName    string
}

func newXDSClientWrapper(loadStore Store) *xdsClientWrapper {
	return &xdsClientWrapper{
		loadStore: loadStore,
	}
}

// update checks the config and xdsclient, and decides whether it needs to
// restart the load reporting stream.
//
// TODO: refactor lrs to share one stream instead of one per EDS.
func (w *xdsClientWrapper) update(newConfig *lbConfig, attr *attributes.Attributes) {
	var restartLoadReport bool
	if attr != nil {
		if clientFromAttr, _ := attr.Value(xdsinternal.XDSClientID).(xdsClientInterface); clientFromAttr != nil {
			if w.c != clientFromAttr {
				// xds client is different, restart.
				restartLoadReport = true
				w.c = clientFromAttr
			}
		}
	}

	// ClusterName is different, restart. ClusterName is from ClusterName and
	// EdsServiceName.
	//
	// TODO: LRS request actually has separate fields from these two values.
	// Update lrs.Store to set both.
	newClusterName := newConfig.EdsServiceName
	if newClusterName == "" {
		newClusterName = newConfig.ClusterName
	}
	if w.clusterName != newClusterName {
		restartLoadReport = true
		w.clusterName = newClusterName
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
		if w.c != nil {
			w.cancelLoadReport = w.c.ReportLoad(w.lrsServerName, w.clusterName, w.loadStore)
		}
	}
}

func (w *xdsClientWrapper) close() {
	if w.cancelLoadReport != nil {
		w.cancelLoadReport()
		w.cancelLoadReport = nil
	}
}
