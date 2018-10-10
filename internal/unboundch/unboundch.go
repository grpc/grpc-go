/*
 *
 * Copyright 2018 gRPC authors.
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

// Package unboundch implements unbound channel
package unboundch

import "sync"

// UnboundChannel is an unbound channel for anything
//
// Function Put is used to put a data to channel.
// Function Get is used to aquire the channel.
// The caller should call Load upon receiving from channel.
type UnboundChannel struct {
	c       chan interface{}
	mu      sync.Mutex
	backlog []interface{}
}

// NewUnboundChannel create an unbound channel
//
// bufsz is initial buff size
func NewUnboundChannel(bufsz int) *UnboundChannel {
	return &UnboundChannel{
		c:       make(chan interface{}, 1),
		backlog: make([]interface{}, 0, bufsz),
	}
}

// Put put a data to channel
func (uc *UnboundChannel) Put(t interface{}) {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if len(uc.backlog) == 0 {
		select {
		case uc.c <- t:
			return
		default:
		}
	}
	uc.backlog = append(uc.backlog, t)
}

// Load load a data from backlog to channel
func (uc *UnboundChannel) Load() {
	uc.mu.Lock()
	defer uc.mu.Unlock()
	if len(uc.backlog) > 0 {
		select {
		case uc.c <- uc.backlog[0]:
			uc.backlog[0] = nil
			uc.backlog = uc.backlog[1:]
		default:
		}
	}
}

// Get returns the channel that the data will be sent to.
//
// Upon receiving, the caller should call Load to send another
// data onto the channel if there is any.
func (uc *UnboundChannel) Get() <-chan interface{} {
	return uc.c
}
