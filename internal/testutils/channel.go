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
	"context"
	"errors"
)

// ErrRecvTimeout is an error to indicate that a receive operation on the
// channel timed out.
var ErrRecvTimeout = errors.New("timed out when waiting for value on channel")

// DefaultChanBufferSize is the default buffer size of the underlying channel.
const DefaultChanBufferSize = 1

// Channel wraps a generic channel and provides a timed receive operation.
type Channel struct {
	ch chan interface{}
}

// Send sends value on the underlying channel.
func (cwt *Channel) Send(value interface{}) {
	cwt.ch <- value
}

// Receive returns the value received on the underlying channel, or
// ErrRecvTimeout if the context expires.
func (cwt *Channel) Receive(ctx context.Context) (interface{}, error) {
	select {
	case <-ctx.Done():
		return nil, ErrRecvTimeout
	case got := <-cwt.ch:
		return got, nil
	}
}

// Replace clears the value on the underlying channel, and sends the new value.
//
// It's expected to be used with a size-1 channel, to only keep the most
// up-to-date item.
func (cwt *Channel) Replace(value interface{}) {
	select {
	case <-cwt.ch:
	default:
	}
	cwt.ch <- value
}

// NewChannel returns a new Channel.
func NewChannel() *Channel {
	return NewChannelWithSize(DefaultChanBufferSize)
}

// NewChannelWithSize returns a new Channel with a buffer of bufSize.
func NewChannelWithSize(bufSize int) *Channel {
	return &Channel{ch: make(chan interface{}, bufSize)}
}
