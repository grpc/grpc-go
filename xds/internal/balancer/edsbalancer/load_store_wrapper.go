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
	"sync"

	"google.golang.org/grpc/xds/internal/client/load"
)

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
	lsw.perCluster = lsw.store.PerCluster(lsw.service, "")
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
