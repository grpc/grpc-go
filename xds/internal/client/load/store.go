/*
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
 */

// Package load provides functionality to record and maintain load data.
package load

import (
	"sync"
	"sync/atomic"
)

const negativeOneUInt64 = ^uint64(0)

// Store is a repository for LB policy implementations to report store load
// data. It is safe for concurrent use.
//
// A zero Store is empty and ready for use.
//
// TODO(easwars): Use regular maps with mutexes instead of sync.Map here. The
// latter is optimized for two common use cases: (1) when the entry for a given
// key is only ever written once but read many times, as in caches that only
// grow, or (2) when multiple goroutines read, write, and overwrite entries for
// disjoint sets of keys. In these two cases, use of a Map may significantly
// reduce lock contention compared to a Go map paired with a separate Mutex or
// RWMutex.
// Neither of these conditions are met here, and we should transition to a
// regular map with a mutex for better type safety.
type Store struct {
	drops            sync.Map // map[string]*uint64
	localityRPCCount sync.Map // map[string]*rpcCountData
}

// Update functions are called by picker for each RPC. To avoid contention, all
// updates are done atomically.

// CallDropped adds one drop record with the given category to store.
func (ls *Store) CallDropped(category string) {
	if ls == nil {
		return
	}

	p, ok := ls.drops.Load(category)
	if !ok {
		tp := new(uint64)
		p, _ = ls.drops.LoadOrStore(category, tp)
	}
	atomic.AddUint64(p.(*uint64), 1)
}

// CallStarted adds one call started record for the given locality.
func (ls *Store) CallStarted(locality string) {
	if ls == nil {
		return
	}

	p, ok := ls.localityRPCCount.Load(locality)
	if !ok {
		tp := newRPCCountData()
		p, _ = ls.localityRPCCount.LoadOrStore(locality, tp)
	}
	p.(*rpcCountData).incrInProgress()
}

// CallFinished adds one call finished record for the given locality.
// For successful calls, err needs to be nil.
func (ls *Store) CallFinished(locality string, err error) {
	if ls == nil {
		return
	}

	p, ok := ls.localityRPCCount.Load(locality)
	if !ok {
		// The map is never cleared, only values in the map are reset. So the
		// case where entry for call-finish is not found should never happen.
		return
	}
	p.(*rpcCountData).decrInProgress()
	if err == nil {
		p.(*rpcCountData).incrSucceeded()
	} else {
		p.(*rpcCountData).incrErrored()
	}
}

// CallServerLoad adds one server load record for the given locality. The
// load type is specified by desc, and its value by val.
func (ls *Store) CallServerLoad(locality, name string, d float64) {
	if ls == nil {
		return
	}

	p, ok := ls.localityRPCCount.Load(locality)
	if !ok {
		// The map is never cleared, only values in the map are reset. So the
		// case where entry for CallServerLoad is not found should never happen.
		return
	}
	p.(*rpcCountData).addServerLoad(name, d)
}

// Data contains all load data reported to the Store since the most recent call
// to Stats().
type Data struct {
	// TotalDrops is the total number of dropped requests.
	TotalDrops uint64
	// Drops is the number of dropped requests per category.
	Drops map[string]uint64
	// LocalityStats contains load reports per locality.
	LocalityStats map[string]LocalityData
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
}

// ServerLoadData contains server load data.
type ServerLoadData struct {
	// Count is the number of load reports.
	Count uint64
	// Sum is the total value of all load reports.
	Sum float64
}

func newStoreData() *Data {
	return &Data{
		Drops:         make(map[string]uint64),
		LocalityStats: make(map[string]LocalityData),
	}
}

// Stats returns and resets all loads reported to the store, except inProgress
// rpc counts.
func (ls *Store) Stats() *Data {
	if ls == nil {
		return nil
	}

	sd := newStoreData()
	ls.drops.Range(func(key, val interface{}) bool {
		d := atomic.SwapUint64(val.(*uint64), 0)
		if d == 0 {
			return true
		}
		sd.TotalDrops += d
		sd.Drops[key.(string)] = d
		return true
	})
	ls.localityRPCCount.Range(func(key, val interface{}) bool {
		countData := val.(*rpcCountData)
		succeeded := countData.loadAndClearSucceeded()
		inProgress := countData.loadInProgress()
		errored := countData.loadAndClearErrored()
		if succeeded == 0 && inProgress == 0 && errored == 0 {
			return true
		}

		ld := LocalityData{
			RequestStats: RequestData{
				Succeeded:  succeeded,
				Errored:    errored,
				InProgress: inProgress,
			},
			LoadStats: make(map[string]ServerLoadData),
		}
		countData.serverLoads.Range(func(key, val interface{}) bool {
			sum, count := val.(*rpcLoadData).loadAndClear()
			if count == 0 {
				return true
			}
			ld.LoadStats[key.(string)] = ServerLoadData{
				Count: count,
				Sum:   sum,
			}
			return true
		})
		sd.LocalityStats[key.(string)] = ld
		return true
	})
	return sd
}

type rpcCountData struct {
	// Only atomic accesses are allowed for the fields.
	succeeded  *uint64
	errored    *uint64
	inProgress *uint64

	// Map from load desc to load data (sum+count). Loading data from map is
	// atomic, but updating data takes a lock, which could cause contention when
	// multiple RPCs try to report loads for the same desc.
	//
	// To fix the contention, shard this map.
	serverLoads sync.Map // map[string]*rpcLoadData
}

func newRPCCountData() *rpcCountData {
	return &rpcCountData{
		succeeded:  new(uint64),
		errored:    new(uint64),
		inProgress: new(uint64),
	}
}

func (rcd *rpcCountData) incrSucceeded() {
	atomic.AddUint64(rcd.succeeded, 1)
}

func (rcd *rpcCountData) loadAndClearSucceeded() uint64 {
	return atomic.SwapUint64(rcd.succeeded, 0)
}

func (rcd *rpcCountData) incrErrored() {
	atomic.AddUint64(rcd.errored, 1)
}

func (rcd *rpcCountData) loadAndClearErrored() uint64 {
	return atomic.SwapUint64(rcd.errored, 0)
}

func (rcd *rpcCountData) incrInProgress() {
	atomic.AddUint64(rcd.inProgress, 1)
}

func (rcd *rpcCountData) decrInProgress() {
	atomic.AddUint64(rcd.inProgress, negativeOneUInt64) // atomic.Add(x, -1)
}

func (rcd *rpcCountData) loadInProgress() uint64 {
	return atomic.LoadUint64(rcd.inProgress) // InProgress count is not clear when reading.
}

func (rcd *rpcCountData) addServerLoad(name string, d float64) {
	loads, ok := rcd.serverLoads.Load(name)
	if !ok {
		tl := newRPCLoadData()
		loads, _ = rcd.serverLoads.LoadOrStore(name, tl)
	}
	loads.(*rpcLoadData).add(d)
}

// Data for server loads (from trailers or oob). Fields in this struct must be
// updated consistently.
//
// The current solution is to hold a lock, which could cause contention. To fix,
// shard serverLoads map in rpcCountData.
type rpcLoadData struct {
	mu    sync.Mutex
	sum   float64
	count uint64
}

func newRPCLoadData() *rpcLoadData {
	return &rpcLoadData{}
}

func (rld *rpcLoadData) add(v float64) {
	rld.mu.Lock()
	rld.sum += v
	rld.count++
	rld.mu.Unlock()
}

func (rld *rpcLoadData) loadAndClear() (s float64, c uint64) {
	rld.mu.Lock()
	s = rld.sum
	rld.sum = 0
	c = rld.count
	rld.count = 0
	rld.mu.Unlock()
	return
}
