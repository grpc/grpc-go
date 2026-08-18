/*
 * Copyright 2026 gRPC authors.
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

package transport

import (
	"errors"
	"net"
	"sync"
	"syscall"
)

// SampleTCPStats is a function hook for sampling TCP socket statistics
// before the underlying network connection is closed.
var SampleTCPStats func(net.Conn) any

// StatsConn wraps a net.Conn to sample socket statistics immediately before
// Close() invalidates the underlying socket file descriptor.
type StatsConn struct {
	net.Conn
	mu    sync.Mutex
	saved any
	once  sync.Once
}

// NewStatsConn returns a new StatsConn wrapping conn.
func NewStatsConn(conn net.Conn) *StatsConn {
	if conn == nil {
		return nil
	}
	if sc, ok := conn.(*StatsConn); ok {
		return sc
	}
	return &StatsConn{Conn: conn}
}

// Close samples TCP statistics prior to closing the underlying net.Conn.
func (c *StatsConn) Close() error {
	c.once.Do(func() {
		if SampleTCPStats != nil {
			c.mu.Lock()
			c.saved = SampleTCPStats(c.Conn)
			c.mu.Unlock()
		}
	})
	return c.Conn.Close()
}

// SyscallConn satisfies syscall.Conn interface if underlying conn supports it.
func (c *StatsConn) SyscallConn() (syscall.RawConn, error) {
	if sc, ok := c.Conn.(syscall.Conn); ok {
		return sc.SyscallConn()
	}
	return nil, errors.New("underlying connection does not implement syscall.Conn")
}

// SavedStats returns the pre-close sampled statistics, if any.
func (c *StatsConn) SavedStats() any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saved
}
