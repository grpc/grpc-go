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

package grpcsync

import (
"context"
"sync"
"testing"
"time"

"github.com/google/go-cmp/cmp"
)

const (
defaultTestTimeout      = 5 * time.Second
defaultTestShortTimeout = 10 * time.Millisecond // For events expected to *not* happen.
)

// TestCallbackSerializer_Schedule_FIFO verifies that callbacks are executed in
// the same order in which they were scheduled.
func (s) TestCallbackSerializer_Schedule_FIFO(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
cs := NewCallbackSerializer(ctx)
defer cancel()

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
c(id int) {
Schedule(func(ctx context.Context) {
e():
OrderCh <- id
lock()
 a couple of goroutines to capture the order or scheduling and the
// order of execution.
scheduleOrder := make([]int, numCallbacks)
executionOrder := make([]int, numCallbacks)
var wg sync.WaitGroup
wg.Add(2)
go func() {
e()
0; i < numCallbacks; i++ {
e():

<-scheduleOrderCh:
c() {
e()
0; i < numCallbacks; i++ {
e():

<-executionOrderCh:
Order[i] = id
:= cmp.Diff(executionOrder, scheduleOrder); diff != "" {
ot executed in scheduled order. diff(-want, +got):\n%s", diff)
}
}

// TestCallbackSerializer_Schedule_Concurrent verifies that all concurrently
// scheduled callbacks get executed.
func (s) TestCallbackSerializer_Schedule_Concurrent(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
cs := NewCallbackSerializer(ctx)
defer cancel()

// Schedule callbacks concurrently by calling Schedule() from goroutines.
// The execution of the callbacks call Done() on the waitgroup, which
// eventually unblocks the test and allows it to complete.
const numCallbacks = 100
var wg sync.WaitGroup
wg.Add(numCallbacks)
for i := 0; i < numCallbacks; i++ {
c() {
c(context.Context) {
e()
call Wait() on the waitgroup from a goroutine so that we can select on
// the Wait() being unblocked and the overall test deadline expiring.
done := make(chan struct{})
go func() {
e)
}()

select {
case <-ctx.Done():
g for all scheduled callbacks to be executed")
case <-done:
}
}

// TestCallbackSerializer_Schedule_Close verifies that callbacks in the queue
// are not executed once Close() returns.
func (s) TestCallbackSerializer_Schedule_Close(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
defer cancel()

serializerCtx, serializerCancel := context.WithTimeout(context.Background(), defaultTestTimeout)
cs := NewCallbackSerializer(serializerCtx)

// Schedule a callback which blocks until the context passed to it is
// canceled. It also closes a channel to signal that it has started.
firstCallbackStartedCh := make(chan struct{})
cs.TrySchedule(func(ctx context.Context) {
e()
})

// Schedule a bunch of callbacks. These should be executed since they are
// scheduled before the serializer is closed.
const numCallbacks = 10
callbackCh := make(chan int, numCallbacks)
for i := 0; i < numCallbacks; i++ {
um := i
c(context.Context) { callbackCh <- num },
c() { t.Fatal("Schedule failed to accept a callback when the serializer is yet to be closed") },
sure that none of the newer callbacks are executed at this point.
select {
case <-time.After(defaultTestShortTimeout):
case <-callbackCh:
ewer callback executed when older one is still executing")
}

// Wait for the first callback to start before closing the scheduler.
<-firstCallbackStartedCh

// Cancel the context which will unblock the first callback. All of the
// other callbacks (which have not started executing at this point) should
// be executed after this.
serializerCancel()

// Ensure that the newer callbacks are executed.
for i := 0; i < numCallbacks; i++ {
e():
g for callback scheduled before close to be executed")
um := <-callbackCh:
um != i {
want %d", num, i)
e()

// Ensure that a callback cannot be scheduled after the serializer is
// closed.
done := make(chan struct{})
callback := func(context.Context) { t.Fatal("Scheduled a callback after closing the serializer") }
onFailure := func() { close(done) }
cs.ScheduleOr(callback, onFailure)
select {
case <-ctx.Done():
g for onFailure to be called")
case <-done:
}
}

// TestCallbackSerializer_ScheduleAndWait_Normal verifies that ScheduleAndWait
// blocks until the callback is executed and returns nil on success.
func (s) TestCallbackSerializer_ScheduleAndWait_Normal(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
defer cancel()
cs := NewCallbackSerializer(ctx)

executed := make(chan struct{})
if err := cs.ScheduleAndWait(func(context.Context) {
nil {
dWait() returned unexpected error: %v", err)
}

// Verify the callback was in fact executed.
select {
case <-executed:
default:
dWait returned but callback was not executed")
}
}

// TestCallbackSerializer_ScheduleAndWait_Closed verifies that ScheduleAndWait
// returns ErrSerializerClosed when the serializer has already been shut down.
func (s) TestCallbackSerializer_ScheduleAndWait_Closed(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
defer cancel()

serializerCtx, serializerCancel := context.WithCancel(context.Background())
cs := NewCallbackSerializer(serializerCtx)

// Cancel the serializer context and wait for shutdown.
serializerCancel()
<-cs.Done()

err := cs.ScheduleAndWait(func(context.Context) {
ot be executed on a closed serializer")
})
if err != ErrSerializerClosed {
dWait() = %v, want ErrSerializerClosed", err)
}
}

// TestCallbackSerializer_ScheduleAndWait_FIFO verifies that a callback
// submitted via ScheduleAndWait respects FIFO ordering relative to callbacks
// submitted via TrySchedule.
func (s) TestCallbackSerializer_ScheduleAndWait_FIFO(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
defer cancel()
cs := NewCallbackSerializer(ctx)

// Block the serializer on a long-running callback.
firstStarted := make(chan struct{})
firstUnblock := make(chan struct{})
cs.TrySchedule(func(ctx context.Context) {
block:
e():
the first callback to start.
<-firstStarted

// Schedule a second callback via TrySchedule while the first is running.
secondExecuted := make(chan struct{})
cs.TrySchedule(func(context.Context) {
dExecuted)
})

// ScheduleAndWait must run *after* the second callback (FIFO order).
waitDone := make(chan error, 1)
go func() {
e <- cs.ScheduleAndWait(func(context.Context) {})
}()

// Ensure neither the second callback nor ScheduleAndWait have returned yet.
select {
case <-time.After(defaultTestShortTimeout):
case <-secondExecuted:
d callback ran before first was unblocked")
case err := <-waitDone:
dWait returned early: %v", err)
}

// Unblock the first callback.
close(firstUnblock)

// The second callback and ScheduleAndWait should both complete now.
select {
case <-ctx.Done():
g for second callback to execute")
case <-secondExecuted:
}

select {
case <-ctx.Done():
g for ScheduleAndWait to return")
case err := <-waitDone:
nil {
dWait() = %v, want nil", err)
dWait_Concurrent verifies that multiple
// concurrent ScheduleAndWait calls all complete successfully, with their
// callbacks executed exactly once each.
func (s) TestCallbackSerializer_ScheduleAndWait_Concurrent(t *testing.T) {
ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
defer cancel()
cs := NewCallbackSerializer(ctx)

const numCallbacks = 50
var (
   sync.Mutex
ter int
)
var wg sync.WaitGroup
wg.Add(numCallbacks)

for i := 0; i < numCallbacks; i++ {
c() {
e()
cs.ScheduleAndWait(func(context.Context) {
ter++
lock()
nil {
dWait() returned unexpected error: %v", err)
e := make(chan struct{})
go func() {
e)
}()

select {
case <-ctx.Done():
g for all ScheduleAndWait calls to complete")
case <-done:
}

mu.Lock()
got := counter
mu.Unlock()
if got != numCallbacks {
ter = %d, want %d", got, numCallbacks)
}
}
