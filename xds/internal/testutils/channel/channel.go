/*
 *
 * Copyright 2019 gRPC authors.
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

// Package channel provides an implementation of a channel with a receive
// timeout, for use in xds tests.
package channel

import (
	"errors"
	"time"
)

// ErrRecvTimeout is an error to indicate that a receive operation on the
// channel timed out.
var ErrRecvTimeout = errors.New("timed out when waiting for value on channel")

const (
	// DefaultRecvTimeout is the default timeout for receive operations on the
	// underlying channel.
	DefaultRecvTimeout = 1 * time.Second
	// DefaultBufferSize is the default buffer size of the underlying channel.
	DefaultBufferSize = 1
)

// WithTimeout wraps a generic channel and provides a timed receive operation.
type WithTimeout struct {
	ch chan interface{}
}

// Send sends value on the underlying channel.
func (cwt *WithTimeout) Send(value interface{}) {
	cwt.ch <- value
}

// TimedReceive returns the value received on the underlying channel, or
// ErrRecvTimeout if timeout amount of time elapsed.
func (cwt *WithTimeout) TimedReceive(timeout time.Duration) (interface{}, error) {
	timer := time.NewTimer(timeout)
	select {
	case <-timer.C:
		return nil, ErrRecvTimeout
	case got := <-cwt.ch:
		timer.Stop()
		return got, nil
	}
}

// Receive returns the value received on the underlying channel, or
// ErrRecvTimeout if DefaultRecvTimeout amount of time elapses.
func (cwt *WithTimeout) Receive() (interface{}, error) {
	return cwt.TimedReceive(DefaultRecvTimeout)
}

// NewChan returns a new WithTimeout channel.
func NewChan() *WithTimeout {
	return &WithTimeout{ch: make(chan interface{}, DefaultBufferSize)}
}
