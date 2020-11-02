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

package edsbalancer

import (
	"fmt"
	"sync"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/internal/grpclog"
	xdsinternal "google.golang.org/grpc/xds/internal"
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

// xdsClientInterface contains only the xds_client methods needed by EDS
// balancer. It's defined so we can override xdsclientNew function in tests.
type xdsClientInterface interface {
	WatchEndpoints(clusterName string, edsCb func(xdsclient.EndpointsUpdate, error)) (cancel func())
	ReportLoad(server string) (loadStore *load.Store, cancel func())
	Close()
}

type loadStoreWrapper struct {
	mu      sync.RWMutex
	service string
	// Both store and perCluster will be nil if load reporting is disabled (EDS
	// response doesn't have LRS server name). Note that methods on Store and
	// perCluster all handle nil, so there's no need to check nil before calling
	// them.
	store      *load.Store
	perCluster load.PerClusterReporter
}

func (lsw *loadStoreWrapper) updateServiceName(service string) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()
	if lsw.service == service {
		return
	}
	lsw.service = service
	lsw.perCluster = lsw.store.PerCluster(lsw.service, "")
}

func (lsw *loadStoreWrapper) updateLoadStore(store *load.Store) {
	lsw.mu.Lock()
	defer lsw.mu.Unlock()
	if store == lsw.store {
		return
	}
	lsw.store = store
	lsw.perCluster = nil
	lsw.perCluster = lsw.store.PerCluster(lsw.service, "")
}

func (lsw *loadStoreWrapper) CallStarted(locality string) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	lsw.perCluster.CallStarted(locality)
}

func (lsw *loadStoreWrapper) CallFinished(locality string, err error) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	lsw.perCluster.CallFinished(locality, err)
}

func (lsw *loadStoreWrapper) CallServerLoad(locality, name string, val float64) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	lsw.perCluster.CallServerLoad(locality, name, val)
}

func (lsw *loadStoreWrapper) CallDropped(category string) {
	lsw.mu.RLock()
	defer lsw.mu.RUnlock()
	lsw.perCluster.CallDropped(category)
}

// xdsclientWrapper is responsible for getting the xds client from attributes or
// creating a new xds client, and start watching EDS. The given callbacks will
// be called with EDS updates or errors.
type xdsClientWrapper struct {
	logger *grpclog.PrefixLogger

	newEDSUpdate func(xdsclient.EndpointsUpdate, error)

	// xdsClient could come from attributes, or created with balancerName.
	xdsClient xdsClientInterface

	// loadWrapper is a wrapper with loadOriginal, with clusterName and
	// edsServiceName. It's used children to report loads.
	loadWrapper *loadStoreWrapper
	// edsServiceName is the edsServiceName currently being watched, not
	// necessary the edsServiceName from service config.
	//
	// If edsServiceName from service config is an empty, this will be user's
	// dial target (because that's what we use to watch EDS).
	//
	// TODO: remove the empty string related behavior, when we switch to always
	// do CDS.
	edsServiceName       string
	cancelEndpointsWatch func()
	loadReportServer     *string // LRS is disabled if loadReporterServer is nil.
	cancelLoadReport     func()
}

// newXDSClientWrapper creates an empty xds_client wrapper that does nothing. It
// can accept xds_client configs, to new/switch xds_client to use.
//
// The given callbacks won't be called until the underlying xds_client is
// working and sends updates.
func newXDSClientWrapper(newEDSUpdate func(xdsclient.EndpointsUpdate, error), logger *grpclog.PrefixLogger) *xdsClientWrapper {
	return &xdsClientWrapper{
		logger:       logger,
		newEDSUpdate: newEDSUpdate,
		loadWrapper:  &loadStoreWrapper{},
	}
}

// updateXDSClient sets xdsClient in wrapper to the correct one based on the
// attributes and service config.
//
// If client is found in attributes, it will be used, but we also need to decide
// whether to close the old client.
// - if old client was created locally (balancerName is not ""), close it and
// replace it
// - if old client was from previous attributes, only replace it, but don't
// close it
//
// If client is not found in attributes, will need to create a new one only if
// the balancerName (from bootstrap file or from service config) changed.
// - if balancer names are the same, do nothing, and return false
// - if balancer names are different, create new one, and return true
func (c *xdsClientWrapper) updateXDSClient(attr *attributes.Attributes) (bool, error) {
	if attr == nil {
		return false, fmt.Errorf("unexported nil attributes, want attributes with xdsClient")
	}
	// TODO: change the way xdsClient is retrieved from attributes. One option
	// is to add helper functions.
	//
	// Or, since xdsClient will become a singleton, this can just call
	// xdsclient.New() instead. And if we decide to do this, do it in Build
	// instead of when handling updates.
	clientFromAttr, _ := attr.Value(xdsinternal.XDSClientID).(xdsClientInterface)
	if clientFromAttr == nil {
		return false, fmt.Errorf("no xdsClient found in attributes")
	}

	if c.xdsClient == clientFromAttr {
		return false, nil
	}
	c.xdsClient = clientFromAttr
	return true, nil
}

// startEndpointsWatch starts the EDS watch. Caller can call this when the
// xds_client is updated, or the edsServiceName is updated.
//
// Note that if there's already a watch in progress, it's not explicitly
// canceled. Because for each xds_client, there should be only one EDS watch in
// progress. So a new EDS watch implicitly cancels the previous one.
//
// This usually means load report needs to be restarted, but this function does
// NOT do that. Caller needs to call startLoadReport separately.
func (c *xdsClientWrapper) startEndpointsWatch() {
	if c.cancelEndpointsWatch != nil {
		c.cancelEndpointsWatch()
	}
	cancelEDSWatch := c.xdsClient.WatchEndpoints(c.edsServiceName, func(update xdsclient.EndpointsUpdate, err error) {
		c.logger.Infof("Watch update from xds-client %p, content: %+v", c.xdsClient, update)
		c.newEDSUpdate(update, err)
	})
	c.logger.Infof("Watch started on resource name %v with xds-client %p", c.edsServiceName, c.xdsClient)
	c.cancelEndpointsWatch = func() {
		cancelEDSWatch()
		c.logger.Infof("Watch cancelled on resource name %v with xds-client %p", c.edsServiceName, c.xdsClient)
	}
}

// startLoadReport starts load reporting. If there's already a load reporting in
// progress, it cancels that.
//
// Caller can cal this when the loadReportServer name changes, but
// edsServiceName doesn't (so we only need to restart load reporting, not EDS
// watch).
func (c *xdsClientWrapper) startLoadReport(loadReportServer *string) *load.Store {
	if c.cancelLoadReport != nil {
		c.cancelLoadReport()
	}
	c.loadReportServer = loadReportServer
	var loadStore *load.Store
	if c.loadReportServer != nil {
		loadStore, c.cancelLoadReport = c.xdsClient.ReportLoad(*c.loadReportServer)
	}
	return loadStore
}

func (c *xdsClientWrapper) loadStore() load.PerClusterReporter {
	if c == nil {
		return nil
	}
	return c.loadWrapper
}

// handleUpdate applies the service config and attributes updates to the client,
// including updating the xds_client to use, and updating the EDS name to watch.
func (c *xdsClientWrapper) handleUpdate(config *EDSConfig, attr *attributes.Attributes) error {
	clientChanged, err := c.updateXDSClient(attr)
	if err != nil {
		return err
	}

	// Need to restart EDS watch when one of the following happens:
	// - the xds_client is updated
	// - the xds_client didn't change, but the edsServiceName changed
	if clientChanged || c.edsServiceName != config.EDSServiceName {
		c.edsServiceName = config.EDSServiceName
		c.startEndpointsWatch()
		// TODO: this update for the LRS service name is too early. It should
		// only apply to the new EDS response. But this is applied to the RPCs
		// before the new EDS response. To fully fix this, the EDS balancer
		// needs to do a graceful switch to another EDS implementation.
		//
		// This is OK for now, because we don't actually expect edsServiceName
		// to change. Fix this (a bigger change) will happen later.
		c.loadWrapper.updateServiceName(c.edsServiceName)
	}

	// Only need to restart load reporting when:
	// - the loadReportServer name changed
	if !equalStringPointers(c.loadReportServer, config.LrsLoadReportingServerName) {
		loadStore := c.startLoadReport(config.LrsLoadReportingServerName)
		c.loadWrapper.updateLoadStore(loadStore)
	}

	return nil
}

func (c *xdsClientWrapper) cancelWatch() {
	c.loadReportServer = nil
	if c.cancelLoadReport != nil {
		c.cancelLoadReport()
	}
	c.edsServiceName = ""
	if c.cancelEndpointsWatch != nil {
		c.cancelEndpointsWatch()
	}
}

func (c *xdsClientWrapper) close() {
	c.cancelWatch()
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
