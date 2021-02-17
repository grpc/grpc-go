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

// Package clusterimpl implements the xds_cluster_impl balancing policy. It
// handles the cluster features (e.g. circuit_breaking, RPC dropping).
//
// Note that it doesn't handle name resolution, which is done by policy
// xds_cluster_resolver.
package clusterimpl

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/loadstore"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

const (
	clusterImplName        = "xds_cluster_impl_experimental"
	defaultRequestCountMax = 1024
)

func init() {
	balancer.Register(clusterImplBB{})
}

var newXDSClient = func() (xdsClientInterface, error) { return xdsclient.New() }

type clusterImplBB struct{}

func (clusterImplBB) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &clusterImplBalancer{
		ClientConn:      cc,
		bOpts:           bOpts,
		closed:          grpcsync.NewEvent(),
		loadWrapper:     loadstore.NewWrapper(),
		pickerUpdateCh:  buffer.NewUnbounded(),
		requestCountMax: defaultRequestCountMax,
	}
	b.logger = prefixLogger(b)

	client, err := newXDSClient()
	if err != nil {
		b.logger.Errorf("failed to create xds-client: %v", err)
		return nil
	}
	b.xdsC = client
	go b.run()

	b.logger.Infof("Created")
	return b
}

func (clusterImplBB) Name() string {
	return clusterImplName
}

func (clusterImplBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

// xdsClientInterface contains only the xds_client methods needed by LRS
// balancer. It's defined so we can override xdsclient in tests.
type xdsClientInterface interface {
	ReportLoad(server string) (*load.Store, func())
	Close()
}

type clusterImplBalancer struct {
	balancer.ClientConn
	bOpts  balancer.BuildOptions
	closed *grpcsync.Event
	logger *grpclog.PrefixLogger
	xdsC   xdsClientInterface

	config           *lbConfig
	childLB          balancer.Balancer
	cancelLoadReport func()
	clusterName      string
	edsServiceName   string
	lrsServerName    string
	loadWrapper      *loadstore.Wrapper

	// childState/drops/requestCounter can only be accessed in run(). And run()
	// is the only goroutine that sends picker to the parent ClientConn. All
	// requests to update picker need to be sent to pickerUpdateCh.
	childState      balancer.State
	drops           []*dropper
	requestCounter  *xdsclient.ServiceRequestsCounter
	requestCountMax uint32
	pickerUpdateCh  *buffer.Unbounded
}

// updateLoadStore checks the config for load store, and decides whether it
// needs to restart the load reporting stream.
func (cib *clusterImplBalancer) updateLoadStore(newConfig *lbConfig) error {
	var updateLoadClusterAndService bool

	// ClusterName is different, restart. ClusterName is from ClusterName and
	// EdsServiceName.
	if cib.clusterName != newConfig.Cluster {
		updateLoadClusterAndService = true
		cib.clusterName = newConfig.Cluster
	}
	if cib.edsServiceName != newConfig.EDSServiceName {
		updateLoadClusterAndService = true
		cib.edsServiceName = newConfig.EDSServiceName
	}
	if updateLoadClusterAndService {
		// This updates the clusterName and serviceName that will be reported
		// for the loads. The update here is too early, the perfect timing is
		// when the picker is updated with the new connection. But from this
		// balancer's point of view, it's impossible to tell.
		//
		// On the other hand, this will almost never happen. Each LRS policy
		// shouldn't get updated config. The parent should do a graceful switch
		// when the clusterName or serviceName is changed.
		cib.loadWrapper.UpdateClusterAndService(cib.clusterName, cib.edsServiceName)
	}

	// Check if it's necessary to restart load report.
	var newLRSServerName string
	if newConfig.LRSLoadReportingServerName != nil {
		newLRSServerName = *newConfig.LRSLoadReportingServerName
	}
	if cib.lrsServerName != newLRSServerName {
		// LrsLoadReportingServerName is different, load should be report to a
		// different server, restart.
		cib.lrsServerName = newLRSServerName
		if cib.cancelLoadReport != nil {
			cib.cancelLoadReport()
			cib.cancelLoadReport = nil
		}
		var loadStore *load.Store
		if cib.xdsC != nil {
			loadStore, cib.cancelLoadReport = cib.xdsC.ReportLoad(cib.lrsServerName)
		}
		cib.loadWrapper.UpdateLoadStore(loadStore)
	}

	return nil
}

func (cib *clusterImplBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	if cib.closed.HasFired() {
		cib.logger.Warningf("xds: received ClientConnState {%+v} after clusterImplBalancer was closed", s)
		return nil
	}

	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}

	// Need to check for potential errors at the beginning of this function, so
	// that on errors, we reject the whole config, instead of applying part of
	// it.
	bb := balancer.Get(newConfig.ChildPolicy.Name)
	if bb == nil {
		return fmt.Errorf("balancer %q not registered", newConfig.ChildPolicy.Name)
	}

	// Update load reporting config. This needs to be done before updating the
	// child policy because we need the loadStore from the updated client to be
	// passed to the ccWrapper, so that the next picker from the child policy
	// will pick up the new loadStore.
	if err := cib.updateLoadStore(newConfig); err != nil {
		return err
	}

	// Compare new drop config. And update picker if it's changed.
	var updatePicker bool
	if cib.config == nil || !equalDropCategories(cib.config.DropCategories, newConfig.DropCategories) {
		cib.drops = make([]*dropper, 0, len(newConfig.DropCategories))
		for _, c := range newConfig.DropCategories {
			cib.drops = append(cib.drops, newDropper(c))
		}
		updatePicker = true
	}

	// Compare cluster name. And update picker if it's changed, because circuit
	// breaking's stream counter will be different.
	if cib.config == nil || cib.config.Cluster != newConfig.Cluster {
		cib.requestCounter = xdsclient.GetServiceRequestsCounter(newConfig.Cluster)
		updatePicker = true
	}
	// Compare upper bound of stream count. And update picker if it's changed.
	// This is also for circuit breaking.
	var newRequestCountMax uint32 = 1024
	if newConfig.MaxConcurrentRequests != nil {
		newRequestCountMax = *newConfig.MaxConcurrentRequests
	}
	if cib.requestCountMax != newRequestCountMax {
		cib.requestCountMax = newRequestCountMax
		updatePicker = true
	}

	if updatePicker {
		cib.pickerUpdateCh.Put(&dropConfigs{
			drops:           cib.drops,
			requestCounter:  cib.requestCounter,
			requestCountMax: cib.requestCountMax,
		})
	}

	// If child policy is a different type, recreate the sub-balancer.
	if cib.config == nil || cib.config.ChildPolicy.Name != newConfig.ChildPolicy.Name {
		if cib.childLB != nil {
			cib.childLB.Close()
		}
		cib.childLB = bb.Build(cib, cib.bOpts)
	}
	cib.config = newConfig

	if cib.childLB == nil {
		// This is not an expected situation, and should be super rare in
		// practice.
		//
		// When this happens, we already applied all the other configurations
		// (drop/circuit breaking), but there's no child policy. This balancer
		// will be stuck, and we report the error to the parent.
		return fmt.Errorf("child policy is nil, this means balancer %q's Build() returned nil", newConfig.ChildPolicy.Name)
	}

	// Addresses and sub-balancer config are sent to sub-balancer.
	return cib.childLB.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  s.ResolverState,
		BalancerConfig: cib.config.ChildPolicy.Config,
	})
}

func (cib *clusterImplBalancer) ResolverError(err error) {
	if cib.closed.HasFired() {
		cib.logger.Warningf("xds: received resolver error {%+v} after clusterImplBalancer was closed", err)
		return
	}

	if cib.childLB != nil {
		cib.childLB.ResolverError(err)
	}
}

func (cib *clusterImplBalancer) UpdateSubConnState(sc balancer.SubConn, s balancer.SubConnState) {
	if cib.closed.HasFired() {
		cib.logger.Warningf("xds: received subconn state change {%+v, %+v} after clusterImplBalancer was closed", sc, s)
		return
	}

	if cib.childLB != nil {
		cib.childLB.UpdateSubConnState(sc, s)
	}
}

func (cib *clusterImplBalancer) Close() {
	if cib.childLB != nil {
		cib.childLB.Close()
		cib.childLB = nil
	}
	cib.xdsC.Close()
	cib.closed.Fire()
	cib.logger.Infof("Shutdown")
}

// Override methods to accept updates from the child LB.

func (cib *clusterImplBalancer) UpdateState(state balancer.State) {
	// Instead of updating parent ClientConn inline, send state to run().
	cib.pickerUpdateCh.Put(state)
}

type dropConfigs struct {
	drops           []*dropper
	requestCounter  *xdsclient.ServiceRequestsCounter
	requestCountMax uint32
}

func (cib *clusterImplBalancer) run() {
	for {
		select {
		case update := <-cib.pickerUpdateCh.Get():
			cib.pickerUpdateCh.Load()
			switch u := update.(type) {
			case balancer.State:
				cib.childState = u
				cib.ClientConn.UpdateState(balancer.State{
					ConnectivityState: cib.childState.ConnectivityState,
					Picker: newDropPicker(cib.childState, &dropConfigs{
						drops:           cib.drops,
						requestCounter:  cib.requestCounter,
						requestCountMax: cib.requestCountMax,
					}, cib.loadWrapper),
				})
			case *dropConfigs:
				cib.drops = u.drops
				cib.requestCounter = u.requestCounter
				if cib.childState.Picker != nil {
					cib.ClientConn.UpdateState(balancer.State{
						ConnectivityState: cib.childState.ConnectivityState,
						Picker:            newDropPicker(cib.childState, u, cib.loadWrapper),
					})
				}
			}
		case <-cib.closed.Done():
			return
		}
	}
}
