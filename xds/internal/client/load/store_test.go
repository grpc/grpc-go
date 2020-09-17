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
 */

package load

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

var (
	dropCategories = []string{"drop_for_real", "drop_for_fun"}
	localities     = []string{"locality-A", "locality-B"}
	errTest        = fmt.Errorf("test error")
)

// rpcData wraps the rpc counts and load data to be pushed to the store.
type rpcData struct {
	start, success, failure int
	serverData              map[string]float64 // Will be reported with successful RPCs.
}

// TestDrops spawns a bunch of goroutines which report drop data. After the
// goroutines have exited, the test dumps the stats from the Store and makes
// sure they are as expected.
func TestDrops(t *testing.T) {
	var (
		drops = map[string]int{
			dropCategories[0]: 30,
			dropCategories[1]: 40,
		}
		wantStoreData = &Data{
			TotalDrops: 70,
			Drops: map[string]uint64{
				dropCategories[0]: 30,
				dropCategories[1]: 40,
			},
		}
	)

	ls := Store{}
	var wg sync.WaitGroup
	for category, count := range drops {
		for i := 0; i < count; i++ {
			wg.Add(1)
			go func(c string) {
				ls.CallDropped(c)
				wg.Done()
			}(category)
		}
	}
	wg.Wait()

	gotStoreData := ls.Stats()
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("store.Stats() returned unexpected diff (-want +got):\n%s", diff)
	}
}

// TestLocalityStats spawns a bunch of goroutines which report rpc and load
// data. After the goroutines have exited, the test dumps the stats from the
// Store and makes sure they are as expected.
func TestLocalityStats(t *testing.T) {
	var (
		localityData = map[string]rpcData{
			localities[0]: {
				start:      40,
				success:    20,
				failure:    10,
				serverData: map[string]float64{"net": 1, "disk": 2, "cpu": 3, "mem": 4},
			},
			localities[1]: {
				start:      80,
				success:    40,
				failure:    20,
				serverData: map[string]float64{"net": 1, "disk": 2, "cpu": 3, "mem": 4},
			},
		}
		wantStoreData = &Data{
			LocalityStats: map[string]LocalityData{
				localities[0]: {
					RequestStats: RequestData{Succeeded: 20, Errored: 10, InProgress: 10},
					LoadStats: map[string]ServerLoadData{
						"net":  {Count: 20, Sum: 20},
						"disk": {Count: 20, Sum: 40},
						"cpu":  {Count: 20, Sum: 60},
						"mem":  {Count: 20, Sum: 80},
					},
				},
				localities[1]: {
					RequestStats: RequestData{Succeeded: 40, Errored: 20, InProgress: 20},
					LoadStats: map[string]ServerLoadData{
						"net":  {Count: 40, Sum: 40},
						"disk": {Count: 40, Sum: 80},
						"cpu":  {Count: 40, Sum: 120},
						"mem":  {Count: 40, Sum: 160},
					},
				},
			},
		}
	)

	ls := Store{}
	var wg sync.WaitGroup
	for locality, data := range localityData {
		wg.Add(data.start)
		for i := 0; i < data.start; i++ {
			go func(l string) {
				ls.CallStarted(l)
				wg.Done()
			}(locality)
		}
		// The calls to CallStarted() need to happen before the other calls are
		// made. Hence the wait here.
		wg.Wait()

		wg.Add(data.success)
		for i := 0; i < data.success; i++ {
			go func(l string, serverData map[string]float64) {
				ls.CallFinished(l, nil)
				for n, d := range serverData {
					ls.CallServerLoad(l, n, d)
				}
				wg.Done()
			}(locality, data.serverData)
		}
		wg.Add(data.failure)
		for i := 0; i < data.failure; i++ {
			go func(l string) {
				ls.CallFinished(l, errTest)
				wg.Done()
			}(locality)
		}
		wg.Wait()
	}

	gotStoreData := ls.Stats()
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("store.Stats() returned unexpected diff (-want +got):\n%s", diff)
	}
}

func TestResetAfterStats(t *testing.T) {
	// Push a bunch of drops, call stats and load stats, and leave inProgress to be non-zero.
	// Dump the stats. Verify expexted
	// Push the same set of loads as before
	// Now dump and verify the newly expected ones.
	var (
		drops = map[string]int{
			dropCategories[0]: 30,
			dropCategories[1]: 40,
		}
		localityData = map[string]rpcData{
			localities[0]: {
				start:      40,
				success:    20,
				failure:    10,
				serverData: map[string]float64{"net": 1, "disk": 2, "cpu": 3, "mem": 4},
			},
			localities[1]: {
				start:      80,
				success:    40,
				failure:    20,
				serverData: map[string]float64{"net": 1, "disk": 2, "cpu": 3, "mem": 4},
			},
		}
		wantStoreData = &Data{
			TotalDrops: 70,
			Drops: map[string]uint64{
				dropCategories[0]: 30,
				dropCategories[1]: 40,
			},
			LocalityStats: map[string]LocalityData{
				localities[0]: {
					RequestStats: RequestData{Succeeded: 20, Errored: 10, InProgress: 10},
					LoadStats: map[string]ServerLoadData{
						"net":  {Count: 20, Sum: 20},
						"disk": {Count: 20, Sum: 40},
						"cpu":  {Count: 20, Sum: 60},
						"mem":  {Count: 20, Sum: 80},
					},
				},
				localities[1]: {
					RequestStats: RequestData{Succeeded: 40, Errored: 20, InProgress: 20},
					LoadStats: map[string]ServerLoadData{
						"net":  {Count: 40, Sum: 40},
						"disk": {Count: 40, Sum: 80},
						"cpu":  {Count: 40, Sum: 120},
						"mem":  {Count: 40, Sum: 160},
					},
				},
			},
		}
	)

	reportLoad := func(ls *Store) {
		for category, count := range drops {
			for i := 0; i < count; i++ {
				ls.CallDropped(category)
			}
		}
		for locality, data := range localityData {
			for i := 0; i < data.start; i++ {
				ls.CallStarted(locality)
			}
			for i := 0; i < data.success; i++ {
				ls.CallFinished(locality, nil)
				for n, d := range data.serverData {
					ls.CallServerLoad(locality, n, d)
				}
			}
			for i := 0; i < data.failure; i++ {
				ls.CallFinished(locality, errTest)
			}
		}
	}

	ls := Store{}
	reportLoad(&ls)
	gotStoreData := ls.Stats()
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("store.Stats() returned unexpected diff (-want +got):\n%s", diff)
	}

	// The above call to Stats() should have reset all load reports except the
	// inProgress rpc count. We are now going to push the same load data into
	// the store. So, we should expect to see twice the count for inProgress.
	for _, l := range localities {
		ls := wantStoreData.LocalityStats[l]
		ls.RequestStats.InProgress *= 2
		wantStoreData.LocalityStats[l] = ls
	}
	reportLoad(&ls)
	gotStoreData = ls.Stats()
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("store.Stats() returned unexpected diff (-want +got):\n%s", diff)
	}
}
