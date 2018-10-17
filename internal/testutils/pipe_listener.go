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

type pipeAddr struct{}

func (p pipeAddr) Network() string { return "pipe" }
func (p pipeAddr) String() string  { return "pipe" }

// PipeListener is a listener with an unbuffered pipe. Each write will complete only once the other side reads.
type PipeListener struct {
	C    chan chan<- net.Conn
	done chan struct{}
}

// NewPipeListener creates a new pipe listener.
func NewPipeListener() *PipeListener {
	return &PipeListener{
		C:    make(chan chan<- net.Conn),
		done: make(chan struct{}),
	}
}

// Accept accepts a connection.
func (p *PipeListener) Accept() (net.Conn, error) {
	var connChan chan<- net.Conn
	select {
	case <-p.done:
		return nil, errors.New("done")
	case connChan = <-p.C:
	}
	c1, c2 := net.Pipe()
	select {
	case <-p.done:
		return nil, errors.New("done")
	case connChan <- c1:
	}
	close(connChan)
	return c2, nil
}

// Close closes the listener.
func (p *PipeListener) Close() error {
	close(p.done)
	// We don't close p.C, because it races with p.done and may be redundant.
	// TODO(deklerk) Should we figure out how to synchronize these so that we can close p.C?
	return nil
}

// Addr returns a pipe addr.
func (p *PipeListener) Addr() net.Addr {
	return pipeAddr{}
}

// Dialer dials a connection.
func (p *PipeListener) Dialer() func(string, time.Duration) (net.Conn, error) {
	return func(string, time.Duration) (net.Conn, error) {
		connChan := make(chan net.Conn)
		select {
		case <-p.done:
			return nil, errors.New("done")
		case p.C <- connChan:
		}
		select {
		case <-p.done:
			return nil, errors.New("done")
		case conn := <-connChan:
			return conn, nil
		}
	}
}
