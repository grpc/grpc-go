/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpcsync"
)

// TestCallbackSerializer_Schedule_FIFO verifies that callbacks are executed in
// the same order in which they were scheduled.
func (s) TestCallbackSerializer_Schedule_FIFO(t *testing.T) {
	cs := NewCallbackSerializer()
	defer cs.Close()

	// We have two channels, one to record the order of scheduling, and the
	// other to record the order of execution. We spawn a bunch of goroutines
	// which record the order of scheduling and call the actual Schedule()
	// method as well.  The callbacks record the order of execution.
	//
	// We need to grab a lock to record order of scheduling to guarantee that
	// the act of recording and the act of calling Schedule() happen atomically.
	const numCallbacks = 100
	var mu sync.Mutex
	scheduleOrderCh := make(chan int, numCallbacks)
	executionOrderCh := make(chan int, numCallbacks)
	for i := 0; i < numCallbacks; i++ {
		go func(id int) {
			mu.Lock()
			defer mu.Unlock()
			scheduleOrderCh <- id
			cs.Schedule(func() { executionOrderCh <- id })
		}(i)
	}

	// Spawn a couple of goroutines to capture the order or scheduling and the
	// order of execution.
	scheduleOrder := make([]int, numCallbacks)
	executionOrder := make([]int, numCallbacks)
	var wg sync.WaitGroup
	wg.Add(2)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	go func() {
		defer wg.Done()
		for i := 0; i < numCallbacks; i++ {
			select {
			case <-ctx.Done():
				return
			case id := <-scheduleOrderCh:
				scheduleOrder[i] = id
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < numCallbacks; i++ {
			select {
			case <-ctx.Done():
				return
			case id := <-executionOrderCh:
				executionOrder[i] = id
			}
		}
	}()
	wg.Wait()

	if diff := cmp.Diff(executionOrder, scheduleOrder); diff != "" {
		t.Fatalf("Callbacks are not executed in scheduled order. diff(-want, +got):\n%s", diff)
	}
}

// TestCallbackSerializer_Schedule_RunToCompletion verifies that all scheduled
// callbacks are run to completion.
func (s) TestCallbackSerializer_Schedule_RunToCompletion(t *testing.T) {
	cs := NewCallbackSerializer()
	defer cs.Close()

	// We have two channels, one to record the order of scheduling, and the
	// other to record the order of execution. We spawn a bunch of goroutines
	// which record the order of scheduling and call the actual Schedule()
	// method as well.  The callbacks record the order of execution.
	//
	// The act of recording the schedule and the act of calling Schedule() are
	// not guaranteed to happen atomically.
	const numCallbacks = 100
	scheduleOrderCh := make(chan int, numCallbacks)
	executionOrderCh := make(chan int, numCallbacks)
	for i := 0; i < numCallbacks; i++ {
		go func(id int) {
			scheduleOrderCh <- id
			cs.Schedule(func() { executionOrderCh <- id })
		}(i)
	}

	// Spawn a couple of goroutines to capture the order or scheduling and the
	// order of execution.
	scheduleOrder := make([]int, numCallbacks)
	executionOrder := make([]int, numCallbacks)
	var wg sync.WaitGroup
	wg.Add(2)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	go func() {
		defer wg.Done()
		for i := 0; i < numCallbacks; i++ {
			select {
			case <-ctx.Done():
				return
			case id := <-scheduleOrderCh:
				scheduleOrder[i] = id
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < numCallbacks; i++ {
			select {
			case <-ctx.Done():
				return
			case id := <-executionOrderCh:
				executionOrder[i] = id
			}
		}
	}()
	wg.Wait()

	sort.Ints(scheduleOrder)
	sort.Ints(executionOrder)
	if diff := cmp.Diff(executionOrder, scheduleOrder); diff != "" {
		t.Fatalf("Not all scheduled callbacks are executed. diff(-want, +got):\n%s", diff)
	}
}

// TestCallbackSerializer_Schedule_Close verifies that callbacks in the queue
// are not executed once Close() returns.
func (s) TestCallbackSerializer_Schedule_Close(t *testing.T) {
	cs := NewCallbackSerializer()

	// Schedule a callback which blocks until signalled by the test to finish.
	firstCallbackStartedCh := make(chan struct{})
	firstCallbackBlockCh := make(chan struct{})
	firstCallbackFinishCh := make(chan struct{})
	cs.Schedule(func() {
		close(firstCallbackStartedCh)
		<-firstCallbackBlockCh
		close(firstCallbackFinishCh)
	})

	// Wait for the first callback to start before scheduling the others.
	<-firstCallbackStartedCh

	// Schedule a bunch of callbacks. These should not be exeuted since the first
	// one started earlier is blocked.
	const numCallbacks = 10
	errCh := make(chan error, numCallbacks)
	closeReturned := grpcsync.NewEvent()
	for i := 0; i < numCallbacks; i++ {
		cs.Schedule(func() {
			if closeReturned.HasFired() {
				errCh <- fmt.Errorf("callback %d executed when not expected to", i)
			}
		})
	}

	// Ensure that none of the newer callbacks are executed at this point.
	select {
	case <-time.After(defaultTestShortTimeout):
	case err := <-errCh:
		t.Fatal(err)
	}

	// Close will block until the first callback finishes. There is no way to
	// ensure that the call to Close() in the below goroutines happens before we
	// unblock the first callback. The best we can do is to ensure that once Close()
	// returns, none of the other callbacks are executed.
	go func() {
		cs.Close()
		closeReturned.Done()
	}()
	close(firstCallbackBlockCh)

	<-firstCallbackFinishCh
	// Ensure that the newer callbacks are not executed.
	select {
	case <-time.After(defaultTestShortTimeout):
	case err := <-errCh:
		t.Fatal(err)
	}
}
