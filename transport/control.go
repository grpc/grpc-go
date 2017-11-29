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

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

const (
	// The default value of flow control window size in HTTP2 spec.
	defaultWindowSize = 65535
	// The initial window size for flow control.
	initialWindowSize             = defaultWindowSize // for an RPC
	infinity                      = time.Duration(math.MaxInt64)
	defaultClientKeepaliveTime    = infinity
	defaultClientKeepaliveTimeout = time.Duration(20 * time.Second)
	defaultMaxStreamsClient       = 100
	defaultMaxConnectionIdle      = infinity
	defaultMaxConnectionAge       = infinity
	defaultMaxConnectionAgeGrace  = infinity
	defaultServerKeepaliveTime    = time.Duration(2 * time.Hour)
	defaultServerKeepaliveTimeout = time.Duration(20 * time.Second)
	defaultKeepalivePolicyMinTime = time.Duration(5 * time.Minute)
	// max window limit set by HTTP2 Specs.
	maxWindowSize = math.MaxInt32
	// defaultLocalSendQuota sets is default value for number of data
	// bytes that each stream can schedule before some of it being
	// flushed out.
	defaultLocalSendQuota = 128 * 1024
)

type direction int

const (
	outgoing direction = iota
	incoming
)

type item interface {
	item()
}

type itemList struct {
	it   item
	next *itemList
}

// The following defines various control items which could flow through
// the control buffer of transport. They represent different aspects of
// control tasks, e.g., flow control, settings, streaming resetting, etc.

type headerFrame struct {
	streamID    uint32
	hf          []hpack.HeaderField
	endStream   bool // Valid on server side.
	startStream bool // Valid on client side.
	// On the client-side f is called when the
	// header frame is sent out.
	f func()
}

func (*headerFrame) item() {}

type cleanupStream struct {
	streamID uint32
	rst      bool
	rstCode  http2.ErrCode
}

func (*cleanupStream) item() {}

type dataFrame struct {
	streamID  uint32
	endStream bool
	h         []byte
	d         []byte
	// onEachWrite is called every time
	// a part of d is written out.
	onEachWrite func()
	// onWriteComplete is called when all
	// of d is written out.
	onWriteComplete func()
}

func (*dataFrame) item() {}

type windowUpdate struct {
	streamID  uint32
	increment uint32
	dir       direction
}

func (*windowUpdate) item() {}

type settings struct {
	ss  []http2.Setting
	dir direction
}

func (*settings) item() {}

type settingsAck struct {
}

func (*settingsAck) item() {}

type resetStream struct {
	streamID uint32
	code     http2.ErrCode
}

func (*resetStream) item() {}

type incomingGoAway struct {
}

func (*incomingGoAway) item() {}

type goAway struct {
	code      http2.ErrCode
	debugData []byte
	headsUp   bool
	closeConn bool
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

type trInFlow struct {
	limit   uint32
	unacked uint32
}

func (f *trInFlow) newLimit(n uint32) uint32 {
	d := n - f.limit
	f.limit = n
	return d
}

func (f *trInFlow) onData(n uint32) (uint32, error) {
	if allowed := f.limit - f.unacked; n > allowed {
		return 0, fmt.Errorf("received %d-bytes data exceeding the allowed %d bytes", n, allowed)
	}
	f.unacked += n
	var w uint32
	if f.unacked > f.limit/4 {
		w = f.unacked
		f.unacked = 0
	}
	return w, nil
}

// inFlow deals with inbound flow control
type inFlow struct {
	limit   uint32
	read    uint32
	unacked uint32
}

// newLimit updates the inflow window to a new value n.
// It assumes that n is always greater than the old limit.
func (f *inFlow) newLimit(n uint32) uint32 {
	d := n - f.limit
	f.limit = n
	return d
}

func (f *inFlow) maybeAdjust(n uint32) uint32 {
	return 0
}

// onData is invoked when some data frame is received. It updates pendingData.
func (f *inFlow) onData(n uint32) error {
	if allowed := f.limit - atomic.LoadUint32(&f.unacked); n > allowed {
		return fmt.Errorf("received %d-bytes data exceeding the limit %d bytes", n, allowed)
	}
	atomic.AddUint32(&f.unacked, n)
	return nil
}

// onRead is invoked when the application reads the data. It returns the window size
// to be sent to the peer.
func (f *inFlow) onRead(n uint32) uint32 {
	f.read += n
	var w uint32
	if f.read > f.limit/4 {
		w = atomic.SwapUint32(&f.unacked, 0)
		f.read = 0
	}
	return w
}
