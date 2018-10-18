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

package testutils

import (
	"errors"
	"net"
	"time"
)

var closedErr = errors.New("closed")

type pipeAddr struct{}

func (p pipeAddr) Network() string { return "pipe" }
func (p pipeAddr) String() string  { return "pipe" }

// pipeListener is a listener with an unbuffered pipe. Each write will complete only once the other side reads. It
// should only be created using NewPipeListener.
type pipeListener struct {
	C chan chan<- net.Conn
	done chan struct{}
}

// NewPipeListener creates a new pipe listener.
func NewPipeListener() *pipeListener {
	return &pipeListener{
		C: make(chan chan<- net.Conn),
		done: make(chan struct{}),
	}
}

// Accept accepts a connection.
func (p *pipeListener) Accept() (net.Conn, error) {
	var connChan chan<- net.Conn
	select {
	case <-p.done:
		return nil, closedErr
	case connChan = <-p.C:
		select {
		case <-p.done:
			close(connChan)
			return nil, closedErr
		default:
		}
	}
	c1, c2 := net.Pipe()
	connChan <- c1
	close(connChan)
	return c2, nil
}

// Close closes the listener.
func (p *pipeListener) Close() error {
	close(p.done)
	return nil
}

// Addr returns a pipe addr.
func (p *pipeListener) Addr() net.Addr {
	return pipeAddr{}
}

// Dialer dials a connection.
func (p *pipeListener) Dialer() func(string, time.Duration) (net.Conn, error) {
	return func(string, time.Duration) (net.Conn, error) {
		connChan := make(chan net.Conn)
		select {
		case p.C <- connChan:
		case <-p.done:
			return nil, closedErr
		}
		conn, ok := <-connChan
		if !ok {
			return nil, closedErr
		}
		return conn, nil
	}
}
