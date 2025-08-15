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

package lrsclient

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc/internal/xds/clients"
	lrsclientinternal "google.golang.org/grpc/internal/xds/clients/lrsclient/internal"
)

var (
	dropCategories = []string{"drop_for_real", "drop_for_fun"}
	localities     = []clients.Locality{{Region: "locality-A"}, {Region: "locality-B"}}
	errTest        = fmt.Errorf("test error")
)

// rpcData wraps the rpc counts and load data to be pushed to the store.
type rpcData struct {
	start, success, failure int
	serverData              map[string]float64 // Will be reported with successful RPCs.
}

func verifyLoadStoreData(wantStoreData, gotStoreData []*loadData) error {
	if diff := cmp.Diff(wantStoreData, gotStoreData, cmpopts.EquateEmpty(), cmp.AllowUnexported(loadData{}, localityData{}, requestData{}, serverLoadData{}), cmpopts.IgnoreFields(loadData{}, "reportInterval"), sortDataSlice); diff != "" {
		return fmt.Errorf("store.stats() returned unexpected diff (-want +got):\n%s", diff)
	}
	return nil
}

// TestDrops spawns a bunch of goroutines which report drop data. After the
// goroutines have exited, the test dumps the stats from the Store and makes
// sure they are as expected.
func TestDrops(t *testing.T) {
	var (
		drops = map[string]int{
			dropCategories[0]: 30,
			dropCategories[1]: 40,
			"":                10,
		}
		wantStoreData = &loadData{
			totalDrops: 80,
			drops: map[string]uint64{
				dropCategories[0]: 30,
				dropCategories[1]: 40,
			},
		}
	)

	ls := PerClusterReporter{}
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

	gotStoreData := ls.stats()
	if err := verifyLoadStoreData([]*loadData{wantStoreData}, []*loadData{gotStoreData}); err != nil {
		t.Error(err)
	}
}

// TestLocalityStats spawns a bunch of goroutines which report rpc and load
// data. After the goroutines have exited, the test dumps the stats from the
// Store and makes sure they are as expected.
func TestLocalityStats(t *testing.T) {
	var (
		ld = map[clients.Locality]rpcData{
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
		wantStoreData = &loadData{
			localityStats: map[clients.Locality]localityData{
				localities[0]: {
					requestStats: requestData{
						succeeded:  20,
						errored:    10,
						inProgress: 10,
						issued:     40,
					},
					loadStats: map[string]serverLoadData{
						"net":  {count: 20, sum: 20},
						"disk": {count: 20, sum: 40},
						"cpu":  {count: 20, sum: 60},
						"mem":  {count: 20, sum: 80},
					},
				},
				localities[1]: {
					requestStats: requestData{
						succeeded:  40,
						errored:    20,
						inProgress: 20,
						issued:     80,
					},
					loadStats: map[string]serverLoadData{
						"net":  {count: 40, sum: 40},
						"disk": {count: 40, sum: 80},
						"cpu":  {count: 40, sum: 120},
						"mem":  {count: 40, sum: 160},
					},
				},
			},
		}
	)

	ls := PerClusterReporter{}
	var wg sync.WaitGroup
	for locality, data := range ld {
		wg.Add(data.start)
		for i := 0; i < data.start; i++ {
			go func(l clients.Locality) {
				ls.CallStarted(l)
				wg.Done()
			}(locality)
		}
		// The calls to callStarted() need to happen before the other calls are
		// made. Hence the wait here.
		wg.Wait()

		wg.Add(data.success)
		for i := 0; i < data.success; i++ {
			go func(l clients.Locality, serverData map[string]float64) {
				ls.CallFinished(l, nil)
				for n, d := range serverData {
					ls.CallServerLoad(l, n, d)
				}
				wg.Done()
			}(locality, data.serverData)
		}
		wg.Add(data.failure)
		for i := 0; i < data.failure; i++ {
			go func(l clients.Locality) {
				ls.CallFinished(l, errTest)
				wg.Done()
			}(locality)
		}
		wg.Wait()
	}

	gotStoreData := ls.stats()
	if err := verifyLoadStoreData([]*loadData{wantStoreData}, []*loadData{gotStoreData}); err != nil {
		t.Error(err)
	}
}

func TestResetAfterStats(t *testing.T) {
	// Push a bunch of drops, call stats and load stats, and leave inProgress to be non-zero.
	// Dump the stats. Verify expected
	// Push the same set of loads as before
	// Now dump and verify the newly expected ones.
	var (
		drops = map[string]int{
			dropCategories[0]: 30,
			dropCategories[1]: 40,
		}
		ld = map[clients.Locality]rpcData{
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
		wantStoreData = &loadData{
			totalDrops: 70,
			drops: map[string]uint64{
				dropCategories[0]: 30,
				dropCategories[1]: 40,
			},
			localityStats: map[clients.Locality]localityData{
				localities[0]: {
					requestStats: requestData{
						succeeded:  20,
						errored:    10,
						inProgress: 10,
						issued:     40,
					},

					loadStats: map[string]serverLoadData{
						"net":  {count: 20, sum: 20},
						"disk": {count: 20, sum: 40},
						"cpu":  {count: 20, sum: 60},
						"mem":  {count: 20, sum: 80},
					},
				},
				localities[1]: {
					requestStats: requestData{
						succeeded:  40,
						errored:    20,
						inProgress: 20,
						issued:     80,
					},

					loadStats: map[string]serverLoadData{
						"net":  {count: 40, sum: 40},
						"disk": {count: 40, sum: 80},
						"cpu":  {count: 40, sum: 120},
						"mem":  {count: 40, sum: 160},
					},
				},
			},
		}
	)

	reportLoad := func(ls *PerClusterReporter) {
		for category, count := range drops {
			for i := 0; i < count; i++ {
				ls.CallDropped(category)
			}
		}
		for locality, data := range ld {
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

	ls := PerClusterReporter{}
	reportLoad(&ls)
	gotStoreData := ls.stats()
	if err := verifyLoadStoreData([]*loadData{wantStoreData}, []*loadData{gotStoreData}); err != nil {
		t.Error(err)
	}

	// The above call to stats() should have reset all load reports except the
	// inProgress rpc count. We are now going to push the same load data into
	// the store. So, we should expect to see twice the count for inProgress.
	for _, l := range localities {
		ls := wantStoreData.localityStats[l]
		ls.requestStats.inProgress *= 2
		wantStoreData.localityStats[l] = ls
	}
	reportLoad(&ls)
	gotStoreData = ls.stats()
	if err := verifyLoadStoreData([]*loadData{wantStoreData}, []*loadData{gotStoreData}); err != nil {
		t.Error(err)
	}
}

var sortDataSlice = cmp.Transformer("SortDataSlice", func(in []*loadData) []*loadData {
	out := append([]*loadData(nil), in...) // Copy input to avoid mutating it
	sort.Slice(out,
		func(i, j int) bool {
			if out[i].cluster < out[j].cluster {
				return true
			}
			if out[i].cluster == out[j].cluster {
				return out[i].service < out[j].service
			}
			return false
		},
	)
	return out
})

// Test all load are returned for the given clusters, and all clusters are
// reported if no cluster is specified.
func TestStoreStats(t *testing.T) {
	var (
		testClusters = []string{"c0", "c1", "c2"}
		testServices = []string{"s0", "s1"}
		testLocality = clients.Locality{Region: "test-locality"}
	)

	store := newLoadStore()
	for _, c := range testClusters {
		for _, s := range testServices {
			store.ReporterForCluster(c, s).CallStarted(testLocality)
			store.ReporterForCluster(c, s).CallServerLoad(testLocality, "abc", 123)
			store.ReporterForCluster(c, s).CallDropped("dropped")
			store.ReporterForCluster(c, s).CallFinished(testLocality, nil)
		}
	}

	wantC0 := []*loadData{
		{
			cluster: "c0", service: "s0",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
		{
			cluster: "c0", service: "s1",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
	}
	// Call Stats with just "c0", this should return data for "c0", and not
	// touch data for other clusters.
	gotC0 := store.stats([]string{"c0"})
	verifyLoadStoreData(wantC0, gotC0)

	wantOther := []*loadData{
		{
			cluster: "c1", service: "s0",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
		{
			cluster: "c1", service: "s1",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
		{
			cluster: "c2", service: "s0",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
		{
			cluster: "c2", service: "s1",
			totalDrops: 1, drops: map[string]uint64{"dropped": 1},
			localityStats: map[clients.Locality]localityData{
				testLocality: {
					requestStats: requestData{succeeded: 1, issued: 1},
					loadStats:    map[string]serverLoadData{"abc": {count: 1, sum: 123}},
				},
			},
		},
	}
	// Call Stats with empty slice, this should return data for all the
	// remaining clusters, and not include c0 (because c0 data was cleared).
	gotOther := store.stats(nil)
	if err := verifyLoadStoreData(wantOther, gotOther); err != nil {
		t.Error(err)
	}
}

// Test the cases that if a cluster doesn't have load to report, its data is not
// appended to the slice returned by Stats().
func TestStoreStatsEmptyDataNotReported(t *testing.T) {
	var (
		testServices = []string{"s0", "s1"}
		testLocality = clients.Locality{Region: "test-locality"}
	)

	store := newLoadStore()
	// "c0"'s RPCs all finish with success.
	for _, s := range testServices {
		store.ReporterForCluster("c0", s).CallStarted(testLocality)
		store.ReporterForCluster("c0", s).CallFinished(testLocality, nil)
	}
	// "c1"'s RPCs never finish (always inprocess).
	for _, s := range testServices {
		store.ReporterForCluster("c1", s).CallStarted(testLocality)
	}

	want0 := []*loadData{
		{
			cluster: "c0", service: "s0",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{succeeded: 1, issued: 1}},
			},
		},
		{
			cluster: "c0", service: "s1",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{succeeded: 1, issued: 1}},
			},
		},
		{
			cluster: "c1", service: "s0",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{inProgress: 1, issued: 1}},
			},
		},
		{
			cluster: "c1", service: "s1",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{inProgress: 1, issued: 1}},
			},
		},
	}
	// Call Stats with empty slice, this should return data for all the
	// clusters.
	got0 := store.stats(nil)
	if err := verifyLoadStoreData(want0, got0); err != nil {
		t.Error(err)
	}

	want1 := []*loadData{
		{
			cluster: "c1", service: "s0",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{inProgress: 1}},
			},
		},
		{
			cluster: "c1", service: "s1",
			localityStats: map[clients.Locality]localityData{
				testLocality: {requestStats: requestData{inProgress: 1}},
			},
		},
	}
	// Call Stats with empty slice again, this should return data only for "c1",
	// because "c0" data was cleared, but "c1" has in-progress RPCs.
	got1 := store.stats(nil)
	if err := verifyLoadStoreData(want1, got1); err != nil {
		t.Error(err)
	}
}

// TestStoreReportInterval verify that the load report interval gets
// calculated at every stats() call and is the duration between start of last
// load reporting to next stats() call.
func TestStoreReportInterval(t *testing.T) {
	originalTimeNow := lrsclientinternal.TimeNow
	t.Cleanup(func() { lrsclientinternal.TimeNow = originalTimeNow })

	// Initial time for reporter creation
	currentTime := time.Now()
	lrsclientinternal.TimeNow = func() time.Time {
		return currentTime
	}

	store := newLoadStore()
	reporter := store.ReporterForCluster("test-cluster", "test-service")
	// Report dummy drop to ensure stats1 is not nil.
	reporter.CallDropped("dummy-category")

	// Update currentTime to simulate the passage of time between the reporter
	// creation and first stats() call.
	currentTime = currentTime.Add(5 * time.Second)
	stats1 := reporter.stats()

	if stats1 == nil {
		t.Fatalf("stats1 is nil after reporting a drop, want non-nil")
	}
	// Verify stats() call calculate the report interval from the time of
	// reporter creation.
	if got, want := stats1.reportInterval, 5*time.Second; got != want {
		t.Errorf("stats1.reportInterval = %v, want %v", stats1.reportInterval, want)
	}

	// Update currentTime to simulate the passage of time between the first
	// and second stats() call.
	currentTime = currentTime.Add(10 * time.Second)
	// Report another dummy drop to ensure stats2 is not nil.
	reporter.CallDropped("dummy-category-2")
	stats2 := reporter.stats()

	if stats2 == nil {
		t.Fatalf("stats2 is nil after reporting a drop, want non-nil")
	}
	// Verify stats() call calculate the report interval from the time of first
	// stats() call.
	if got, want := stats2.reportInterval, 10*time.Second; got != want {
		t.Errorf("stats2.reportInterval = %v, want %v", stats2.reportInterval, want)
	}
}
