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
	"sync"
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

// inFlow deals with inbound flow control
type inFlow struct {
	mu sync.Mutex
	// The inbound flow control limit for pending data.
	limit uint32
	// pendingData is the overall data which have been received but not been
	// consumed by applications.
	pendingData uint32
	// The amount of data the application has consumed but grpc has not sent
	// window update for them. Used to reduce window update frequency.
	pendingUpdate uint32
	// delta is the extra window update given by receiver when an application
	// is reading data bigger in size than the inFlow limit.
	delta uint32
}

// newLimit updates the inflow window to a new value n.
// It assumes that n is always greater than the old limit.
func (f *inFlow) newLimit(n uint32) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := n - f.limit
	f.limit = n
	return d
}

func (f *inFlow) maybeAdjust(n uint32) uint32 {
	if n > uint32(math.MaxInt32) {
		n = uint32(math.MaxInt32)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// estSenderQuota is the receiver's view of the maximum number of bytes the sender
	// can send without a window update.
	estSenderQuota := int32(f.limit - (f.pendingData + f.pendingUpdate))
	// estUntransmittedData is the maximum number of bytes the sends might not have put
	// on the wire yet. A value of 0 or less means that we have already received all or
	// more bytes than the application is requesting to read.
	estUntransmittedData := int32(n - f.pendingData) // Casting into int32 since it could be negative.
	// This implies that unless we send a window update, the sender won't be able to send all the bytes
	// for this message. Therefore we must send an update over the limit since there's an active read
	// request from the application.
	if estUntransmittedData > estSenderQuota {
		// Sender's window shouldn't go more than 2^31 - 1 as speecified in the HTTP spec.
		if f.limit+n > maxWindowSize {
			f.delta = maxWindowSize - f.limit
		} else {
			// Send a window update for the whole message and not just the difference between
			// estUntransmittedData and estSenderQuota. This will be helpful in case the message
			// is padded; We will fallback on the current available window(at least a 1/4th of the limit).
			f.delta = n
		}
		return f.delta
	}
	return 0
}

// onData is invoked when some data frame is received. It updates pendingData.
func (f *inFlow) onData(n uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendingData += n
	if f.pendingData+f.pendingUpdate > f.limit+f.delta {
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
	if n > f.delta {
		n -= f.delta
		f.delta = 0
	} else {
		f.delta -= n
		n = 0
	}
	f.pendingUpdate += n
	if f.pendingUpdate >= f.limit/4 {
		wu := f.pendingUpdate
		f.pendingUpdate = 0
		return wu
	}
	return 0
}

func (f *inFlow) resetPendingUpdate() uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.pendingUpdate
	f.pendingUpdate = 0
	return n
}
