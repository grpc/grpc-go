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

package channelz

import (
	"net"
	"time"

	"google.golang.org/grpc/credentials"
)

// SocketMetric defines the info channelz provides for a specific Socket, which
// includes SocketInternalMetric and channelz-specific data, such as channelz id, etc.
type SocketMetric struct {
	// ID is the channelz id of this socket.
	ID int64
	// RefName is the human readable reference string of this socket.
	RefName string
	// SocketData contains socket internal metric reported by the socket through
	// ChannelzMetric().
	SocketData *SocketInternalMetric
}

// SocketInternalMetric defines the struct that the implementor of Socket interface
// should return from ChannelzMetric().
type SocketInternalMetric struct {
	// The number of streams that have been started.
	StreamsStarted int64
	// The number of streams that have ended successfully:
	// On client side, receiving frame with eos bit set.
	// On server side, sending frame with eos bit set.
	StreamsSucceeded int64
	// The number of streams that have ended unsuccessfully:
	// On client side, termination without receiving frame with eos bit set.
	// On server side, termination without sending frame with eos bit set.
	StreamsFailed int64
	// The number of messages successfully sent on this socket.
	MessagesSent     int64
	MessagesReceived int64
	// The number of keep alives sent.  This is typically implemented with HTTP/2
	// ping messages.
	KeepAlivesSent int64
	// The last time a stream was created by this endpoint.  Usually unset for
	// servers.
	LastLocalStreamCreatedTimestamp time.Time
	// The last time a stream was created by the remote endpoint.  Usually unset
	// for clients.
	LastRemoteStreamCreatedTimestamp time.Time
	// The last time a message was sent by this endpoint.
	LastMessageSentTimestamp time.Time
	// The last time a message was received by this endpoint.
	LastMessageReceivedTimestamp time.Time
	// The amount of window, granted to the local endpoint by the remote endpoint.
	// This may be slightly out of date due to network latency.  This does NOT
	// include stream level or TCP level flow control info.
	LocalFlowControlWindow int64
	// The amount of window, granted to the remote endpoint by the local endpoint.
	// This may be slightly out of date due to network latency.  This does NOT
	// include stream level or TCP level flow control info.
	RemoteFlowControlWindow int64
	// The locally bound address.
	LocalAddr net.Addr
	// The remote bound address.  May be absent.
	RemoteAddr net.Addr
	// Optional, represents the name of the remote endpoint, if different than
	// the original target name.
	RemoteName    string
	SocketOptions *SocketOptionData
	Security      credentials.ChannelzSecurityValue
}

// Socket is the interface that should be satisfied in order to be tracked by
// channelz as Socket.
type Socket interface {
	ChannelzMetric() *SocketInternalMetric
}

type listenSocket struct {
	refName string
	s       Socket
	id      int64
	pid     int64
	cm      *channelMap
}

func (ls *listenSocket) addChild(id int64, e entry) {
	logger.Errorf("cannot add a child (id = %d) of type %T to a listen socket", id, e)
}

func (ls *listenSocket) deleteChild(id int64) {
	logger.Errorf("cannot delete a child (id = %d) from a listen socket", id)
}

func (ls *listenSocket) triggerDelete() {
	ls.cm.deleteEntry(ls.id)
	ls.cm.findEntry(ls.pid).deleteChild(ls.id)
}

func (ls *listenSocket) deleteSelfIfReady() {
	logger.Errorf("cannot call deleteSelfIfReady on a listen socket")
}

func (ls *listenSocket) getParentID() int64 {
	return ls.pid
}

type normalSocket struct {
	refName string
	s       Socket
	id      int64
	pid     int64
	cm      *channelMap
}

func (ns *normalSocket) addChild(id int64, e entry) {
	logger.Errorf("cannot add a child (id = %d) of type %T to a normal socket", id, e)
}

func (ns *normalSocket) deleteChild(id int64) {
	logger.Errorf("cannot delete a child (id = %d) from a normal socket", id)
}

func (ns *normalSocket) triggerDelete() {
	ns.cm.deleteEntry(ns.id)
	ns.cm.findEntry(ns.pid).deleteChild(ns.id)
}

func (ns *normalSocket) deleteSelfIfReady() {
	logger.Errorf("cannot call deleteSelfIfReady on a normal socket")
}

func (ns *normalSocket) getParentID() int64 {
	return ns.pid
}
