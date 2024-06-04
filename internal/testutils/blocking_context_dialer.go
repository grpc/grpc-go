/*
 *
 * Copyright 2024 gRPC authors.
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

package testutils

import (
	"context"
	"net"
)

// BlockingDialer is a dialer that waits for Resume() to be called before
// dialing.
type BlockingDialer struct {
	dialer  *net.Dialer
	blockCh chan struct{}
}

// NewBlockingDialer returns a dialer that waits for Resume() to be called
// before dialing.
func NewBlockingDialer() *BlockingDialer {
	return &BlockingDialer{
		dialer:  &net.Dialer{},
		blockCh: make(chan struct{}),
	}
}

// DialContext implements a context dialer for use with grpc.WithContextDialer
// dial option for a BlockingDialer.
func (d *BlockingDialer) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	select {
	case <-d.blockCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return d.dialer.DialContext(ctx, "tcp", addr)
}

// Resume unblocks the dialer. It panics if called more than once.
func (d *BlockingDialer) Resume() {
	close(d.blockCh)
}
