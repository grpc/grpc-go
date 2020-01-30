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

package adaptive

import (
	"sync"
	"testing"
	"time"
)

// Enums for responses.
const (
	E = iota // No response
	A        // Accepted
	T        // Throttled
)

func TestRegisterBackendResponse(t *testing.T) {
	testcases := []struct {
		desc          string
		bins          int64
		ticks         []int64
		responses     []int64
		wantAccepts   []int64
		wantThrottled []int64
	}{
		{
			"Accumulate",
			3,
			[]int64{0, 1, 2}, // Ticks
			[]int64{A, T, E}, // Responses
			[]int64{1, 1, 1}, // Accepts
			[]int64{0, 1, 1}, // Throttled
		},
		{
			"LightTimeTravel",
			3,
			[]int64{1, 0, 2}, // Ticks
			[]int64{A, T, E}, // Response
			[]int64{1, 1, 1}, // Accepts
			[]int64{0, 1, 1}, // Throttled
		},
		{
			"HeavyTimeTravel",
			3,
			[]int64{8, 0, 9}, // Ticks
			[]int64{A, A, A}, // Response
			[]int64{1, 1, 2}, // Accepts
			[]int64{0, 0, 0}, // Throttled
		},
		{
			"Rollover",
			1,
			[]int64{0, 1, 2}, // Ticks
			[]int64{A, T, E}, // Responses
			[]int64{1, 0, 0}, // Accepts
			[]int64{0, 1, 0}, // Throttled
		},
	}

	m := mockClock{}
	oldTimeNowFunc := timeNowFunc
	timeNowFunc = m.Now
	defer func() { timeNowFunc = oldTimeNowFunc }()

	for _, test := range testcases {
		t.Run(test.desc, func(t *testing.T) {
			th := newWithArgs(time.Duration(test.bins), test.bins, 2.0, 8)
			for i, tick := range test.ticks {
				m.SetNanos(tick)

				if test.responses[i] != E {
					th.RegisterBackendResponse(test.responses[i] == T)
				}
				gotRequests, gotThrottled := th.stats()
				gotAccepts := gotRequests - gotThrottled

				if gotAccepts != test.wantAccepts[i] {
					t.Errorf("allowed for index %d got %d, want %d", i, gotAccepts, test.wantAccepts[i])
				}
				if gotThrottled != test.wantThrottled[i] {
					t.Errorf("throttled for index %d got %d, want %d", i, gotThrottled, test.wantThrottled[i])
				}
			}
		})
	}
}

func TestShouldThrottleOptions(t *testing.T) {
	// ShouldThrottle should return true iff
	//    (requests - RatioForAccepts * accepts) / (requests + RequestsPadding) <= p
	// where p is a random number. For the purposes of this test it's fixed
	// to 0.5.
	responses := []int64{T, T, T, T, T, T, T, T, T, A, A, A, A, A, A, T, T, T, T}

	n := false
	y := true

	testcases := []struct {
		desc            string
		ratioForAccepts float64
		requestsPadding float64
		want            []bool
	}{
		{
			"Baseline",
			1.1,
			8,
			[]bool{n, n, n, n, n, n, n, n, y, y, y, y, y, n, n, n, y, y, y},
		},
		{
			"ChangePadding",
			1.1,
			7,
			[]bool{n, n, n, n, n, n, n, y, y, y, y, y, y, y, y, y, y, y, y},
		},
		{
			"ChangeRatioForAccepts",
			1.4,
			8,
			[]bool{n, n, n, n, n, n, n, n, y, y, n, n, n, n, n, n, n, n, n},
		},
	}

	m := mockClock{}
	oldTimeNowFunc := timeNowFunc
	timeNowFunc = m.Now
	oldRandFunc := randFunc
	randFunc = func() float64 { return 0.5 }
	defer func() {
		timeNowFunc = oldTimeNowFunc
		randFunc = oldRandFunc
	}()

	for _, test := range testcases {
		t.Run(test.desc, func(t *testing.T) {
			m.SetNanos(0)
			th := newWithArgs(time.Duration(time.Nanosecond), 1, test.ratioForAccepts, test.requestsPadding)
			for i, response := range responses {
				if response != E {
					th.RegisterBackendResponse(response == T)
				}
				if got := th.ShouldThrottle(); got != test.want[i] {
					t.Errorf("ShouldThrottle for index %d: got %v, want %v", i, got, test.want[i])
				}
			}
		})
	}
}

func TestParallel(t *testing.T) {
	// Uses all the defaults which comes with a 30 second duration.
	th := New()

	testDuration := 2 * time.Second
	numRoutines := 10
	counts := make([]int64, numRoutines)
	var wg sync.WaitGroup
	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go func(num int) {
			defer wg.Done()

			ticker := time.NewTicker(testDuration)
			var count int64
			for {
				select {
				case <-ticker.C:
					ticker.Stop()
					counts[num] = count
					return
				default:
					th.RegisterBackendResponse(true)
					count++
				}
			}
		}(i)
	}
	wg.Wait()

	var total int64
	for i := range counts {
		total += counts[i]
	}

	gotRequests, gotThrottled := th.stats()
	if gotRequests != total || gotThrottled != total {
		t.Errorf("got requests = %d, throtled = %d, want %d for both", gotRequests, gotThrottled, total)
	}
}

type mockClock struct {
	t time.Time
}

func (m *mockClock) Now() time.Time {
	return m.t
}

func (m *mockClock) SetNanos(n int64) {
	m.t = time.Unix(0, n)
}
