//revive:disable:unused-parameter

/*
 *
 * Copyright 2025 gRPC authors.
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

package lrsclient

import "time"

// LoadStore keeps the loads for multiple clusters and services to be reported
// via LRS. It contains loads to report to one LRS server. It creates
// multiple stores for multiple servers.
//
// A LoadStore is created via LRSClient.ReportLoad and returned for the caller
// to report loads.
type LoadStore struct {
}

// Stats returns the load data for the given cluster names. Data is returned in
// a slice with no specific order.
//
// If no clusterName is given (an empty slice), all data for all known clusters
// is returned.
//
// If a cluster's Data is empty (no load to report), it's not appended to the
// returned slice.
func (s *LoadStore) Stats(clusterNames []string) []*Data {
	panic("unimplemented")
}

// PerCluster returns the perClusterStore for the given clusterName +
// serviceName.
func (s *LoadStore) PerCluster(clusterName, serviceName string) PerClusterReporter {
	panic("unimplemented")
}

// Data contains all load data reported to the LoadStore since the most recent
// call to Stats().
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
	// ReportInternal is the duration since last time load was reported (Stats()
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

// PerClusterReporter wraps the methods from the LoadStore that are used here.
type PerClusterReporter interface {
	CallStarted(locality string)
	CallFinished(locality string, err error)
	CallServerLoad(locality, name string, val float64)
	CallDropped(category string)
}
