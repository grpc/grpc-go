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

package clusterimpl

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/client/load"
)

var newRandomWRR = wrr.NewRandom

const million = 1000000

type dropper struct {
	category string
	w        wrr.WRR
}

// greatest common divisor (GCD) via Euclidean algorithm
func gcd(a, b uint32) uint32 {
	for b != 0 {
		t := b
		b = a % b
		a = t
	}
	return a
}

func newDropper(c dropCategory) *dropper {
	w := newRandomWRR()
	gcdv := gcd(c.RequestsPerMillion, million)
	// Return true for RequestPerMillion, false for the rest.
	w.Add(true, int64(c.RequestsPerMillion/gcdv))
	w.Add(false, int64((million-c.RequestsPerMillion)/gcdv))

	return &dropper{
		category: c.Category,
		w:        w,
	}
}

func (d *dropper) drop() (ret bool) {
	return d.w.Next().(bool)
}

// loadReporter wraps the methods from the loadStore that are used here.
type loadReporter interface {
	CallDropped(locality string)
}

type dropPicker struct {
	drops     []*dropper
	s         balancer.State
	loadStore loadReporter
	counter   *client.ServiceRequestsCounter
	// TODO: add maxRequestCount for circuit breaking.
}

func newDropPicker(s balancer.State, drops []*dropper, loadStore load.PerClusterReporter, counter *client.ServiceRequestsCounter) *dropPicker {
	return &dropPicker{
		drops:     drops,
		s:         s,
		loadStore: loadStore,
		counter:   counter,
	}
}

func (d *dropPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	// Don't drop unless the inner picker is READY. Similar to
	// https://github.com/grpc/grpc-go/issues/2622.
	if d.s.ConnectivityState != connectivity.Ready {
		return d.s.Picker.Pick(info)
	}

	var (
		drop     bool
		category string
	)
	for _, dp := range d.drops {
		if dp.drop() {
			drop = true
			category = dp.category
			break
		}
	}
	if drop {
		if d.loadStore != nil {
			d.loadStore.CallDropped(category)
		}
		return balancer.PickResult{}, status.Errorf(codes.Unavailable, "RPC is dropped")
	}
	// TODO: support circuit breaking, check if d.maxRequestCount >=
	// d.counter.StartRequestWithMax().
	return d.s.Picker.Pick(info)
}
