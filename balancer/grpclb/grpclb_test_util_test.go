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
 *
 */

package grpclb

import (
	"net"
	"sync"
	"sync/atomic"
)

type tempError struct{}

func (*tempError) Error() string {
	return "grpclb test temporary error"
}
func (*tempError) Temporary() bool {
	return true
}

type restartableListener struct {
	net.Listener

	closed uint32

	addr string

	mu    sync.Mutex
	conns []net.Conn
}

func newRestartableListener(l net.Listener) *restartableListener {
	return &restartableListener{
		Listener: l,
		addr:     l.Addr().String(),
	}
}

func (l *restartableListener) Accept() (conn net.Conn, err error) {
	if atomic.LoadUint32(&l.closed) == 1 {
		return nil, &tempError{}
	}

	conn, err = l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.conns = append(l.conns, conn)
		l.mu.Unlock()
	}
	return
}

func (l *restartableListener) Close() error {
	l.Listener.Close()
	l.stop()
	return nil
}

func (l *restartableListener) stop() {
	l.mu.Lock()
	tmp := l.conns
	l.conns = nil
	l.mu.Unlock()
	for _, conn := range tmp {
		conn.Close()
	}
	atomic.StoreUint32(&l.closed, 1)
}

func (l *restartableListener) restart() {
	atomic.StoreUint32(&l.closed, 0)
}
