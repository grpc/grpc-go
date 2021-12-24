/*
 *
 * Copyright 2014 gRPC authors.
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

package transport

import (
	"testing"
	"time"
)

func TestWriteQuotaRaceCondition(t *testing.T) {
	done := make(chan struct{})

	wq := newWriteQuota(5, done)

	// Acquire all of the space in the write quota
	// so that subsequent acquirers will be blocked
	// until replenish.
	if err := wq.get(5); err != nil {
		t.Fatal(err)
	}

	acquirer1 := make(chan struct{})
	acquirer2 := make(chan struct{})

	go func() {
		if err := wq.get(5); err != nil {
			panic(err)
		}
		close(acquirer1)
	}()
	go func() {
		if err := wq.get(5); err != nil {
			panic(err)
		}
		close(acquirer2)
	}()

	// Add a sleep statement to ensure that both goroutines are blocked
	// on [w.ch].
	time.Sleep(time.Second)
	wq.replenish(20)

	finished := make(chan struct{})
	go func() {
		<-acquirer1
		<-acquirer2
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("replenish failed to unblock concurrently waiting goroutines")
	}
}
