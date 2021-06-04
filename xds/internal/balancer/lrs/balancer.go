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

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/loadstore"
	"google.golang.org/grpc/xds/internal/xdsclient"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
)

func init() {
	balancer.Register(bb{})
}

// Name is the name of the LRS balancer.
const Name = "lrs_experimental"

type bb struct{}

func (bb) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	b := &lrsBalancer{
		cc:        cc,
		buildOpts: opts,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
	return b
}

func (bb) Name() string {
	return Name
}

func (bb) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

type lrsBalancer struct {
	cc        balancer.ClientConn
	buildOpts balancer.BuildOptions

	logger    *grpclog.PrefixLogger
	xdsClient *xdsClientWrapper

	config *LBConfig
	lb     balancer.Balancer // The sub balancer.
}

func (b *lrsBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	b.logger.Infof("Received update from resolver, balancer config: %+v", pretty.ToJSON(s.BalancerConfig))
	newConfig, ok := s.BalancerConfig.(*LBConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}

	if b.xdsClient == nil {
		c := xdsclient.FromResolverState(s.ResolverState)
		if c == nil {
			return balancer.ErrBadResolverState
		}
		b.xdsClient = newXDSClientWrapper(c)
	}

	// Update load reporting config or xds client. This needs to be done before
	// updating the child policy because we need the loadStore from the updated
	// client to be passed to the ccWrapper.
	if err := b.xdsClient.update(newConfig); err != nil {
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
		ccWrapper := newCCWrapper(b.cc, b.xdsClient.loadStore(), lidJSON)
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
	b.xdsClient.close()
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

type xdsClientWrapper struct {
	c                xdsclient.XDSClient
	cancelLoadReport func()
	clusterName      string
	edsServiceName   string
	lrsServerName    *string
	// loadWrapper is a wrapper with loadOriginal, with clusterName and
	// edsServiceName. It's used children to report loads.
	loadWrapper *loadstore.Wrapper
}

func newXDSClientWrapper(c xdsclient.XDSClient) *xdsClientWrapper {
	return &xdsClientWrapper{
		c:           c,
		loadWrapper: loadstore.NewWrapper(),
	}
}

// update checks the config and xdsclient, and decides whether it needs to
// restart the load reporting stream.
func (w *xdsClientWrapper) update(newConfig *LBConfig) error {
	var (
		restartLoadReport           bool
		updateLoadClusterAndService bool
	)

	// ClusterName is different, restart. ClusterName is from ClusterName and
	// EDSServiceName.
	if w.clusterName != newConfig.ClusterName {
		updateLoadClusterAndService = true
		w.clusterName = newConfig.ClusterName
	}
	if w.edsServiceName != newConfig.EDSServiceName {
		updateLoadClusterAndService = true
		w.edsServiceName = newConfig.EDSServiceName
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
		w.loadWrapper.UpdateClusterAndService(w.clusterName, w.edsServiceName)
	}

	if w.lrsServerName == nil || *w.lrsServerName != newConfig.LoadReportingServerName {
		// LoadReportingServerName is different, load should be report to a
		// different server, restart.
		restartLoadReport = true
		w.lrsServerName = &newConfig.LoadReportingServerName
	}

	if restartLoadReport {
		if w.cancelLoadReport != nil {
			w.cancelLoadReport()
			w.cancelLoadReport = nil
		}
		var loadStore *load.Store
		if w.c != nil {
			loadStore, w.cancelLoadReport = w.c.ReportLoad(*w.lrsServerName)
		}
		w.loadWrapper.UpdateLoadStore(loadStore)
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
}
