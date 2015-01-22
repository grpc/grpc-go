/*
 *
 * Copyright 2014, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package transport

import (
	"sync"

	"github.com/bradfitz/http2"
)

// TODO(zhaoq): Make the following configurable.
const (
	// The initial window size for flow control.
	initialWindowSize = 65535
	// Window update is only sent when the inbound quota reaches
	// this threshold. Used to reduce the flow control traffic.
	windowUpdateThreshold = 16384
)

// The following defines various control items which could flow through
// the control buffer of transport. They represent different aspects of
// control tasks, e.g., flow control, settings, streaming resetting, etc.
type windowUpdate struct {
	streamID  uint32
	increment uint32
}

func (windowUpdate) isItem() bool {
	return true
}

type settings struct {
	id  http2.SettingID
	val uint32
}

func (settings) isItem() bool {
	return true
}

type resetStream struct {
	streamID uint32
	code     http2.ErrCode
}

func (resetStream) isItem() bool {
	return true
}

// quotaPool is a pool which accumulates the quota and sends it to acquire()
// when it is available.
type quotaPool struct {
	c chan int

	mu    sync.Mutex
	quota int
}

// newQuotaPool creates a quotaPool which has quota q available to consume.
func newQuotaPool(q int) *quotaPool {
	qb := &quotaPool{c: make(chan int, 1)}
	qb.c <- q
	return qb
}

// add adds n to the available quota and tries to send it on acquire.
func (qb *quotaPool) add(n int) {
	qb.mu.Lock()
	defer qb.mu.Unlock()
	qb.quota += n
	if qb.quota <= 0 {
		return
	}
	select {
	case qb.c <- qb.quota:
		qb.quota = 0
	default:
	}
}

// cancel cancels the pending quota sent on acquire, if any.
func (qb *quotaPool) cancel() {
	qb.mu.Lock()
	defer qb.mu.Unlock()
	select {
	case n := <-qb.c:
		qb.quota += n
	default:
	}
}

// acquire returns the channel on which available quota amounts are sent.
func (qb *quotaPool) acquire() <-chan int {
	return qb.c
}
