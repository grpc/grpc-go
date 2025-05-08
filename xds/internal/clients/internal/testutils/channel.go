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

// Package testutils contains testing helpers for xDS and LRS clients.
package testutils

import "context"

// Channel wraps a generic channel and provides a timed receive operation.
type Channel struct {
	// C is the underlying channel on which values sent using the SendXxx()
	// methods are delivered. Tests which cannot use ReceiveXxx() for whatever
	// reasons can use C to read the values.
	C chan any
}

// Send sends value on the underlying channel.
func (c *Channel) Send(value any) {
	c.C <- value
}

// Receive returns the value received on the underlying channel, or the error
// returned by ctx if it is closed or cancelled.
func (c *Channel) Receive(ctx context.Context) (any, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case got := <-c.C:
		return got, nil
	}
}

// Replace clears the value on the underlying channel, and sends the new value.
//
// It's expected to be used with a size-1 channel, to only keep the most
// up-to-date item. This method is inherently racy when invoked concurrently
// from multiple goroutines.
func (c *Channel) Replace(value any) {
	for {
		select {
		case c.C <- value:
			return
		case <-c.C:
		}
	}
}

// ReceiveOrFail returns the value on the underlying channel and true, or nil
// and false if the channel was empty.
func (c *Channel) ReceiveOrFail() (any, bool) {
	select {
	case got := <-c.C:
		return got, true
	default:
		return nil, false
	}
}

// NewChannelWithSize returns a new Channel with a buffer of bufSize.
func NewChannelWithSize(bufSize int) *Channel {
	return &Channel{C: make(chan any, bufSize)}
}
