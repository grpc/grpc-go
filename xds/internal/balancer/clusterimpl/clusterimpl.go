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
// handles the features in cluster except name resolution. Features include
// circuit_breaking, RPC dropping.
package clusterimpl

import (
	"encoding/json"
	"fmt"
	"reflect"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/buffer"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
	"google.golang.org/grpc/xds/internal/balancer/utils"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

const (
	clusterImplName        = "xds_cluster_impl_experimental"
	defaultRequestCountMax = 1024
)

func init() {
	balancer.Register(&clusterImplBB{})
}

var newXDSClient = func() (xdsClientInterface, error) { return xdsclient.New() }

type clusterImplBB struct{}

func (wt *clusterImplBB) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	b := &clusterImplBalancer{
		cc:              cc,
		closed:          grpcsync.NewEvent(),
		loadWrapper:     utils.NewLoadStoreWrapper(),
		pickerUpdateCh:  buffer.NewUnbounded(),
		requestCountMax: defaultRequestCountMax,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")

	client, err := newXDSClient()
	if err != nil {
		b.logger.Errorf("failed to create xds-client: %v", err)
		return nil
	}
	b.c = client

	return b
}

func (wt *clusterImplBB) Name() string {
	return clusterImplName
}

func (wt *clusterImplBB) ParseConfig(c json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	return parseConfig(c)
}

// xdsClientInterface contains only the xds_client methods needed by LRS
// balancer. It's defined so we can override xdsclient in tests.
type xdsClientInterface interface {
	ReportLoad(server string) (*load.Store, func())
	Close()
}

type clusterImplBalancer struct {
	cc     balancer.ClientConn
	closed *grpcsync.Event

	logger *grpclog.PrefixLogger
	c      xdsClientInterface

	config *lbConfig
	lb     balancer.Balancer

	cancelLoadReport func()
	clusterName      string
	edsServiceName   string
	lrsServerName    string
	loadWrapper      *utils.LoadStoreWrapper

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
	var (
		restartLoadReport           bool
		updateLoadClusterAndService bool
	)

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
		// This updates the clusterName and serviceName that will reported for the
		// loads. The update here is too early, the perfect timing is when the
		// picker is updated with the new connection. But from this balancer's point
		// of view, it's impossible to tell.
		//
		// On the other hand, this will almost never happen. Each LRS policy
		// shouldn't get updated config. The parent should do a graceful switch when
		// the clusterName or serviceName is changed.
		cib.loadWrapper.UpdateClusterAndService(cib.clusterName, cib.edsServiceName)
	}

	var newLRSServerName string
	if newConfig.LRSLoadReportingServerName != nil {
		newLRSServerName = *newConfig.LRSLoadReportingServerName
	}
	if cib.lrsServerName != newLRSServerName {
		// LrsLoadReportingServerName is different, load should be report to a
		// different server, restart.
		restartLoadReport = true
		cib.lrsServerName = newLRSServerName
	}

	if restartLoadReport {
		if cib.cancelLoadReport != nil {
			cib.cancelLoadReport()
			cib.cancelLoadReport = nil
		}
		var loadStore *load.Store
		if cib.c != nil {
			loadStore, cib.cancelLoadReport = cib.c.ReportLoad(cib.lrsServerName)
		}
		cib.loadWrapper.UpdateLoadStore(loadStore)
	}

	return nil
}

func (cib *clusterImplBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	newConfig, ok := s.BalancerConfig.(*lbConfig)
	if !ok {
		return fmt.Errorf("unexpected balancer config with type: %T", s.BalancerConfig)
	}

	// Update load reporting config. This needs to be done before updating the
	// child policy because we need the loadStore from the updated client to be
	// passed to the ccWrapper.
	if err := cib.updateLoadStore(newConfig); err != nil {
		return err
	}

	var updatePicker bool
	if cib.config == nil || !reflect.DeepEqual(cib.config.DropCategories, newConfig.DropCategories) {
		cib.drops = make([]*dropper, 0, len(newConfig.DropCategories))
		for _, c := range newConfig.DropCategories {
			cib.drops = append(cib.drops, newDropper(c))
		}
		updatePicker = true
	}

	if cib.config == nil || cib.config.Cluster != newConfig.Cluster {
		cib.requestCounter = xdsclient.GetServiceRequestsCounter(newConfig.Cluster)
		updatePicker = true
	}

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
			drops:          cib.drops,
			requestCounter: cib.requestCounter,
		})
	}

	// If child policy is a different type, recreate the sub-balancer.
	if cib.config == nil || cib.config.ChildPolicy.Name != newConfig.ChildPolicy.Name {
		bb := balancer.Get(newConfig.ChildPolicy.Name)
		if bb == nil {
			return fmt.Errorf("balancer %q not registered", newConfig.ChildPolicy.Name)
		}
		if cib.lb != nil {
			cib.lb.Close()
		}
		cib.lb = bb.Build(cib, balancer.BuildOptions{})
	}
	cib.config = newConfig

	// Addresses and sub-balancer config are sent to sub-balancer.
	return cib.lb.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  s.ResolverState,
		BalancerConfig: cib.config.ChildPolicy.Config,
	})
}

func (cib *clusterImplBalancer) ResolverError(err error) {
	if cib.lb != nil {
		cib.lb.ResolverError(err)
	}
}

func (cib *clusterImplBalancer) UpdateSubConnState(sc balancer.SubConn, s balancer.SubConnState) {
	if cib.lb != nil {
		cib.lb.UpdateSubConnState(sc, s)
	}
}

func (cib *clusterImplBalancer) Close() {
	if cib.lb != nil {
		cib.lb.Close()
		cib.lb = nil
	}
	cib.c.Close()
	cib.closed.Fire()
	cib.logger.Infof("Shutdown")
}

func (cib *clusterImplBalancer) UpdateState(state balancer.State) {
	// Instead of updating parent ClientConn inline, send state to run().
	cib.pickerUpdateCh.Put(state)
}

func (cib *clusterImplBalancer) NewSubConn(addresses []resolver.Address, options balancer.NewSubConnOptions) (balancer.SubConn, error) {
	return cib.cc.NewSubConn(addresses, options)
}

func (cib *clusterImplBalancer) RemoveSubConn(conn balancer.SubConn) {
	cib.cc.RemoveSubConn(conn)
}

func (cib *clusterImplBalancer) ResolveNow(options resolver.ResolveNowOptions) {
	cib.cc.ResolveNow(options)
}

func (cib *clusterImplBalancer) Target() string {
	return cib.cc.Target()
}

type dropConfigs struct {
	drops          []*dropper
	requestCounter *xdsclient.ServiceRequestsCounter
}

func (cib *clusterImplBalancer) run() {
	for {
		select {
		case update := <-cib.pickerUpdateCh.Get():
			cib.pickerUpdateCh.Load()
			// x.handleGRPCUpdate(update)
			switch u := update.(type) {
			case balancer.State:
				cib.childState = u
				cib.cc.UpdateState(balancer.State{
					ConnectivityState: cib.childState.ConnectivityState,
					Picker:            newDropPicker(cib.childState.Picker, cib.drops, cib.loadWrapper, cib.requestCounter),
				})
			case *dropConfigs:
				cib.drops = u.drops
				cib.requestCounter = u.requestCounter
				cib.cc.UpdateState(balancer.State{
					ConnectivityState: cib.childState.ConnectivityState,
					Picker:            newDropPicker(cib.childState.Picker, cib.drops, cib.loadWrapper, cib.requestCounter),
				})
			}
		case <-cib.closed.Done():
			return
		}
	}
}
