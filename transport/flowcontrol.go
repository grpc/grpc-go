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
	"fmt"
	"math"
	"sync/atomic"
	"time"
)

const (
	// The default value of flow control window size in HTTP2 spec.
	defaultWindowSize = 65535
	// The initial window size for flow control.
	initialWindowSize             = defaultWindowSize // for an RPC
	infinity                      = time.Duration(math.MaxInt64)
	defaultClientKeepaliveTime    = infinity
	defaultClientKeepaliveTimeout = 20 * time.Second
	defaultMaxStreamsClient       = 100
	defaultMaxConnectionIdle      = infinity
	defaultMaxConnectionAge       = infinity
	defaultMaxConnectionAgeGrace  = infinity
	defaultServerKeepaliveTime    = 2 * time.Hour
	defaultServerKeepaliveTimeout = 20 * time.Second
	defaultKeepalivePolicyMinTime = 5 * time.Minute
	// max window limit set by HTTP2 Specs.
	maxWindowSize = math.MaxInt32
	// defaultWriteQuota is the default value for number of data
	// bytes that each stream can schedule before some of it being
	// flushed out.
	defaultWriteQuota = 64 * 1024
)

// writeQuota is a soft limit on the amount of data a stream can
// schedule before some of it is written out.
type writeQuota struct {
	quota int32
	// get waits on read from when quota goes less than or equal to zero.
	// replenish writes on it when quota goes positive again.
	ch chan struct{}
	// done is triggered in error case.
	done <-chan struct{}
}

func newWriteQuota(sz int32, done <-chan struct{}) *writeQuota {
	return &writeQuota{
		quota: sz,
		ch:    make(chan struct{}, 1),
		done:  done,
	}
}

func (w *writeQuota) get(sz int32) error {
	for {
		if atomic.LoadInt32(&w.quota) > 0 {
			atomic.AddInt32(&w.quota, -sz)
			return nil
		}
		select {
		case <-w.ch:
			continue
		case <-w.done:
			return errStreamDone
		}
	}
}

func (w *writeQuota) replenish(n int) {
	sz := int32(n)
	a := atomic.AddInt32(&w.quota, sz)
	b := a - sz
	if b <= 0 && a > 0 {
		select {
		case w.ch <- struct{}{}:
		default:
		}
	}
}

type trInFlow struct {
	limit               uint32 // accessed by reader goroutine.
	unacked             uint32 // accessed by reader goroutine.
	effectiveWindowSize uint32 // accessed by reader and channelz request goroutine.
	// Callback used to schedule window update.
	scheduleWU func(uint32)
}

// Sets the new limit.
func (f *trInFlow) newLimit(n uint32) {
	if n > f.limit {
		f.scheduleWU(n - f.limit)
	}
	f.limit = n
	f.updateEffectiveWindowSize()
}

func (f *trInFlow) onData(n uint32) {
	f.unacked += n
	if f.unacked >= f.limit/4 {
		w := f.unacked
		f.unacked = 0
		f.scheduleWU(w)
	}
	f.updateEffectiveWindowSize()
}

func (f *trInFlow) reset() {
	if f.unacked == 0 {
		return
	}
	f.scheduleWU(f.unacked)
	f.unacked = 0
	f.updateEffectiveWindowSize()
}

func (f *trInFlow) updateEffectiveWindowSize() {
	atomic.StoreUint32(&f.effectiveWindowSize, f.limit-f.unacked)
}

func (f *trInFlow) getSize() uint32 {
	return atomic.LoadUint32(&f.effectiveWindowSize)
}

// stInFlow deals with inbound flow control for stream.
// It can be simultaneously read by transport's reader
// goroutine and an RPC's goroutine.
// It is protected by the lock in stream that owns it.
type stInFlow struct {
	// rcvd is the bytes of data that this end-point has
	// received from the perspective of other side.
	// This can go negative. It must be Accessed atomically.
	// Needs to be aligned because of golang bug with atomics:
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	rcvd int64
	// The inbound flow control limit for pending data.
	limit uint32
	// number of bytes received so far, this should be accessed
	// number of bytes that have been read by the RPC.
	read uint32
	// a window update should be sent when the RPC has
	// read these many bytes.
	// TODO(mmukhi, dfawley): Does this have to be limit/4?
	// Keeping it a constant makes implementation easy.
	wuThreshold uint32
	// Callback used to schedule window update.
	scheduleWU func(uint32)
}

// called by transport's reader goroutine to set a new limit on
// incoming flow control based on BDP estimation.
func (s *stInFlow) newLimit(n uint32) {
	s.limit = n
}

// called by transport's reader goroutine when data is received by it.
func (s *stInFlow) onData(n uint32) error {
	rcvd := atomic.AddInt64(&s.rcvd, int64(n))
	if rcvd > int64(s.limit) { // Flow control violation.
		return fmt.Errorf("received %d-bytes data exceeding the limit %d bytes", rcvd, s.limit)
	}
	return nil
}

// called by RPC's goroutine when data is read by it.
func (s *stInFlow) onRead(n uint32) {
	s.read += n
	if s.read >= s.wuThreshold {
		val := atomic.AddInt64(&s.rcvd, ^int64(s.read-1))
		// Check if threshold needs to go up since limit might have gone up.
		val += int64(s.read)
		if val > int64(4*s.wuThreshold) {
			s.wuThreshold = uint32(val / 4)
		}
		s.scheduleWU(s.read)
		s.read = 0
	}
}
