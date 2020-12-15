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

package resolver

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	_ "google.golang.org/grpc/xds/internal/balancer/cdsbalancer" // To parse LB config
)

func (s) TestPruneActiveClusters(t *testing.T) {
	r := &xdsResolver{activeClusters: map[string]*clusterInfo{
		"zero":        {refCount: 0},
		"one":         {refCount: 1},
		"two":         {refCount: 2},
		"anotherzero": {refCount: 0},
	}}
	want := map[string]*clusterInfo{
		"one": {refCount: 1},
		"two": {refCount: 2},
	}
	r.pruneActiveClusters()
	if d := cmp.Diff(r.activeClusters, want, cmp.AllowUnexported(clusterInfo{})); d != "" {
		t.Fatalf("r.activeClusters = %v; want %v\nDiffs: %v", r.activeClusters, want, d)
	}
}
