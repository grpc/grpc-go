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
 */

package testutils

import (
	"errors"
	"time"
)

// ErrRecvTimeout is an error to indicate that a receive operation on the
// channel timed out.
var ErrRecvTimeout = errors.New("timed out when waiting for value on channel")

const (
	// DefaultChanRecvTimeout is the default timeout for receive operations on the
	// underlying channel.
	DefaultChanRecvTimeout = 1 * time.Second
	// DefaultChanBufferSize is the default buffer size of the underlying channel.
	DefaultChanBufferSize = 1
)

// Channel wraps a generic channel and provides a timed receive operation.
type Channel struct {
	ch chan interface{}
}

// Send sends value on the underlying channel.
func (cwt *Channel) Send(value interface{}) {
	cwt.ch <- value
}

// Receive returns the value received on the underlying channel, or
// ErrRecvTimeout if DefaultChanRecvTimeout amount of time elapses.
func (cwt *Channel) Receive() (interface{}, error) {
	timer := time.NewTimer(DefaultChanRecvTimeout)
	select {
	case <-timer.C:
		return nil, ErrRecvTimeout
	case got := <-cwt.ch:
		timer.Stop()
		return got, nil
	}
}

// NewChannel returns a new Channel.
func NewChannel() *Channel {
	return NewChannelWithSize(DefaultChanBufferSize)
}

// NewChannelWithSize returns a new Channel with a buffer of bufSize.
func NewChannelWithSize(bufSize int) *Channel {
	return &Channel{ch: make(chan interface{}, bufSize)}
}
