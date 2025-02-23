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

// LoadStore keep track of the loads for multiple clusters and services that
// are intended to be reported via LRS.
//
// LoadStore stores loads reported to a single LRS server. Use multiple stores
// for multiple servers.
//
// It is safe for concurrent use.
type LoadStore struct {
}

// PerCluster returns the PerClusterReporter for the given cluster and service.
func (ls *LoadStore) PerCluster(clusterName, serviceName string) PerClusterReporter {
	panic("unimplemented")
}

// PerClusterReporter defines the methods that the LoadStore uses to track
// per-cluster load reporting data.
//
// The lrsclient package provides an implementation of this which can be used
// to push loads to the received LoadStore from the LRS client.
type PerClusterReporter interface {
	// CallStarted records a call started in the LoadStore.
	CallStarted(locality string)
	// CallFinished records a call finished in the LoadStore.
	CallFinished(locality string, err error)
	// CallServerLoad records the server load in the LoadStore.
	CallServerLoad(locality, name string, val float64)
	// CallDropped records a call dropped in the LoadStore.
	CallDropped(category string)
}
