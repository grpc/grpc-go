/*
 *
 * Copyright 2017 gRPC authors.
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

package channelz

import (
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/connectivity"
)

type entryType int

const (
	topChannelT entryType = iota
	subChannelT
	nestedChannelT
	serverT
	normalSocketT
	listenSocketT
)

type conn interface {
	Type() entryType
	sync.Locker
}

// ChannelMetric defines the metrics collected for a Channel.
type ChannelMetric struct {
	ID                       int64
	RefName                  string
	State                    connectivity.State
	Target                   string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
	NestedChans map[int64]string
	SubChans    map[int64]string
	Sockets     map[int64]string
}

// Channel is the interface implemented by grpc.ClientConn and grpc.addrConn
// that reports ChannelMetric.
type Channel interface {
	ChannelzMetrics() *ChannelMetric
}

type channel struct {
	t           entryType
	c           Channel
	mu          sync.Mutex
	nestedChans map[int64]string
	subChans    map[int64]string
	sockets     map[int64]string
	pid         int64
}

func (c *channel) Type() entryType {
	return c.t
}

func (c *channel) Lock() {
	c.mu.Lock()
}

func (c *channel) Unlock() {
	c.mu.Unlock()
}

// SocketMetric defines the metrics collected for a Socket.
type SocketMetric struct {
	ID                               int64
	RefName                          string
	StreamsStarted                   int64
	StreamsSucceeded                 int64
	StreamsFailed                    int64
	MessagesSent                     int64
	MessagesReceived                 int64
	KeepAlivesSent                   int64
	LastLocalStreamCreatedTimestamp  time.Time
	LastRemoteStreamCreatedTimestamp time.Time
	LastMessageSentTimestamp         time.Time
	LastMessageReceivedTimestamp     time.Time
	LocalFlowControlWindow           int64
	RemoteFlowControlWindow          int64
	//socket options
	Local  net.Addr
	Remote net.Addr
	// Security
	RemoteName string
}

// Socket is the interface implemented by transport.http2Client and transport.http2Server.
type Socket interface {
	ChannelzMetrics() *SocketMetric
	IncrMsgSent()
	IncrMsgRecv()
	SetID(int64)
}

type socket struct {
	t    entryType
	name string
	s    Socket
	pid  int64
}

func (s *socket) Type() entryType {
	return s.t
}

func (s *socket) Lock() {
}

func (s *socket) Unlock() {
}

// ServerMetric defines the metrics collected for a Server.
type ServerMetric struct {
	ID                       int64
	RefName                  string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace

	ListenSockets map[int64]string
}

// Server is the interface implemented by grpc.Server that reports ServerMetric.
type Server interface {
	ChannelzMetrics() *ServerMetric
}

type server struct {
	name          string
	s             Server
	mu            sync.Mutex
	sockets       map[int64]string
	listenSockets map[int64]string
}

func (*server) Type() entryType {
	return serverT
}

func (s *server) Lock() {
	s.mu.Lock()
}

func (s *server) Unlock() {
	s.mu.Unlock()
}
