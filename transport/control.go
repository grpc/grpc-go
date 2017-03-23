/*
 *
 * Copyright 2014, gRPC authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
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
	"fmt"
	"math"
	"sync"
	"time"

	"golang.org/x/net/http2"
)

const (
	// The default value of flow control window size in HTTP2 spec.
	defaultWindowSize = 65535
	// The initial window size for flow control.
	initialWindowSize             = defaultWindowSize      // for an RPC
	initialConnWindowSize         = defaultWindowSize * 16 // for a connection
	infinity                      = time.Duration(math.MaxInt64)
	defaultClientKeepaliveTime    = infinity
	defaultClientKeepaliveTimeout = time.Duration(20 * time.Second)
	defaultMaxStreamsClient       = 100
	defaultMaxConnectionIdle      = infinity
	defaultMaxConnectionAge       = infinity
	defaultMaxConnectionAgeGrace  = infinity
	defaultServerKeepaliveTime    = time.Duration(2 * time.Hour)
	defaultServerKeepaliveTimeout = time.Duration(20 * time.Second)
)

// The following defines various control items which could flow through
// the control buffer of transport. They represent different aspects of
// control tasks, e.g., flow control, settings, streaming resetting, etc.
type windowUpdate struct {
	streamID  uint32
	increment uint32
}

func (*windowUpdate) item() {}

type settings struct {
	ack bool
	ss  []http2.Setting
}

func (*settings) item() {}

type resetStream struct {
	streamID uint32
	code     http2.ErrCode
}

func (*resetStream) item() {}

type goAway struct {
}

func (*goAway) item() {}

type flushIO struct {
}

func (*flushIO) item() {}

type ping struct {
	ack  bool
	data [8]byte
}

func (*ping) item() {}

// quotaPool is a pool which accumulates the quota and sends it to acquire()
// when it is available.
type quotaPool struct {
	c chan int

	mu    sync.Mutex
	quota int
}

// newQuotaPool creates a quotaPool which has quota q available to consume.
func newQuotaPool(q int) *quotaPool {
	qb := &quotaPool{
		c: make(chan int, 1),
	}
	if q > 0 {
		qb.c <- q
	} else {
		qb.quota = q
	}
	return qb
}

// add cancels the pending quota sent on acquired, incremented by v and sends
// it back on acquire.
func (qb *quotaPool) add(v int) {
	qb.mu.Lock()
	defer qb.mu.Unlock()
	select {
	case n := <-qb.c:
		qb.quota += n
	default:
	}
	qb.quota += v
	if qb.quota <= 0 {
		return
	}
	// After the pool has been created, this is the only place that sends on
	// the channel. Since mu is held at this point and any quota that was sent
	// on the channel has been retrieved, we know that this code will always
	// place any positive quota value on the channel.
	select {
	case qb.c <- qb.quota:
		qb.quota = 0
	default:
	}
}

// acquire returns the channel on which available quota amounts are sent.
func (qb *quotaPool) acquire() <-chan int {
	return qb.c
}

// inFlow deals with inbound flow control
type inFlow struct {
	// The inbound flow control limit for pending data.
	limit uint32

	mu sync.Mutex
	// pendingData is the overall data which have been received but not been
	// consumed by applications.
	pendingData uint32
	// The amount of data the application has consumed but grpc has not sent
	// window update for them. Used to reduce window update frequency.
	pendingUpdate uint32
}

// onData is invoked when some data frame is received. It updates pendingData.
func (f *inFlow) onData(n uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendingData += n
	if f.pendingData+f.pendingUpdate > f.limit {
		return fmt.Errorf("received %d-bytes data exceeding the limit %d bytes", f.pendingData+f.pendingUpdate, f.limit)
	}
	return nil
}

// onRead is invoked when the application reads the data. It returns the window size
// to be sent to the peer.
func (f *inFlow) onRead(n uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pendingData == 0 {
		return 0
	}
	f.pendingData -= n
	f.pendingUpdate += n
	if f.pendingUpdate >= f.limit/4 {
		wu := f.pendingUpdate
		f.pendingUpdate = 0
		return wu
	}
	return 0
}

func (f *inFlow) resetPendingData() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.pendingData
	f.pendingData = 0
	return n
}
