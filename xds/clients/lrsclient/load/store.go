/*
 *
 * Copyright 2024 gRPC authors.
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

package load

import (
	"sync"
	"time"
)

// Store keeps the loads for multiple clusters and services to be reported via
// LRS. It contains loads to report to one LRS server. Create multiple stores
// for multiple servers.
//
// It is safe for concurrent use.
type Store struct {
	// clusters is a map with cluster name as the key. The second layer is a
	// map with service name as the key. Each value (perClusterStore) contains
	// data for a (cluster, service) pair.
	//
	// Note that new entries are added to this map, but never removed. This is
	// potentially a memory leak. But the memory is allocated for each new
	// (cluster,service) pair, and the memory allocated is just pointers and
	// maps. So this shouldn't get too bad.
	clusters map[string]map[string]*PerClusterStore
}

// New creates a Store.
func New() *Store {
	return &Store{
		clusters: make(map[string]map[string]*PerClusterStore),
	}
}

// PerClusterStore is a repository to report store load data.
// It contains load for a (cluster, edsService) pair.
//
// It is safe for concurrent use.
type PerClusterStore struct {
	cluster, service string
	drops            sync.Map // map[string]*uint64
	localityRPCCount sync.Map // map[string]*rpcCountData
	lastLoadReportAt time.Time
}

// Data contains all load data reported to the Store since the most recent call
// to stats().
type Data struct {
	// Cluster is the name of the cluster this data is for.
	Cluster string
	// Service is the name of the EDS service this data is for.
	Service string
	// TotalDrops is the total number of dropped requests.
	TotalDrops uint64
	// Drops is the number of dropped requests per category.
	Drops map[string]uint64
	// LocalityStats contains load reports per locality.
	LocalityStats map[string]LocalityData
	// ReportInternal is the duration since last time load was reported (stats()
	// was called).
	ReportInterval time.Duration
}

// LocalityData contains load data for a single locality.
type LocalityData struct {
	// RequestStats contains counts of requests made to the locality.
	RequestStats RequestData
	// LoadStats contains server load data for requests made to the locality,
	// indexed by the load type.
	LoadStats map[string]ServerLoadData
}

// RequestData contains request counts.
type RequestData struct {
	// Succeeded is the number of succeeded requests.
	Succeeded uint64
	// Errored is the number of requests which ran into errors.
	Errored uint64
	// InProgress is the number of requests in flight.
	InProgress uint64
	// Issued is the total number requests that were sent.
	Issued uint64
}

// ServerLoadData contains server load data.
type ServerLoadData struct {
	// Count is the number of load reports.
	Count uint64
	// Sum is the total value of all load reports.
	Sum float64
}

// Stats returns the load data for the given cluster names. Data is returned in
// a slice with no specific order.
//
// If no clusterName is given (an empty slice), all data for all known clusters
// is returned.
//
// If a cluster's Data is empty (no load to report), it's not appended to the
// returned slice.
func (s *Store) Stats(clusterNames []string) []*Data {
	return []*Data{}
}

// PerCluster returns the PerClusterStore for the given clusterName +
// serviceName.
func (s *Store) PerCluster(clusterName, serviceName string) PerClusterStore {
	return PerClusterStore{}
}

// CallDropped adds one drop record with the given category to store.
func (ls *PerClusterStore) CallDropped(category string) {
}

// CallStarted adds one call started record for the given locality.
func (ls *PerClusterStore) CallStarted(locality string) {
}

// CallFinished adds one call finished record for the given locality.
// For successful calls, err needs to be nil.
func (ls *PerClusterStore) CallFinished(locality string, err error) {
}

// CallServerLoad adds one server load record for the given locality. The
// load type is specified by desc, and its value by val.
func (ls *PerClusterStore) CallServerLoad(locality, name string, d float64) {
}
