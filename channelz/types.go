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

	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/grpclog"
)

type entry interface {
	addChild(id int64, e entry)
	deleteChild(id int64)
	delete()
	canDelete()
}

type dummyEntry struct {
	idNotFound int64
}

func (d *dummyEntry) addChild(id int64, e entry) {
	// It is possible for a normal program to reach here under race condition.
	// For example, there could be a race between ClientConn.Close() info being propagated
	// to addrConn and http2Client. ClientConn.Close() cancel the context and result
	// in http2Client to error. The error info is then caught by transport monitor
	// and before addrConn.tearDown() is called in side ClientConn.Close(). Therefore,
	// the addrConn will create a new transport. And when registering the new transport in
	// channelz, its parent addrConn could have already been torn down and deleted
	// from channelz tracking, and thus reach the code here.
	grpclog.Infof("attempt to add child of type %T with id %d to a parent (id=%d) that doesn't currently exist", e, id, d.idNotFound)
}

func (d *dummyEntry) deleteChild(id int64) {
	// It is possible for a normal program to reach here under race condition.
	// Refer to the example described in addChild().
	grpclog.Infof("attempt to delete child with id %d from a parent (id=%d) that doesn't currently exist", id, d.idNotFound)
}

func (*dummyEntry) delete()    {}
func (*dummyEntry) canDelete() {}

// ChannelMetric defines the info provided by channelz for a specific Channel, which
// includes ChannelInternalMetric and channelz-specific data, i.e, Channelz ID, etc.
type ChannelMetric struct {
	ID          int64
	RefName     string
	ChannelData *ChannelInternalMetric
	NestedChans map[int64]string
	SubChans    map[int64]string
	Sockets     map[int64]string
}

// SubChannelMetric defines the info provided by channelz for a specific SubChannel,
// which includes ChannelInternalMetric and channelz-specific data, i.e, Channelz ID, etc.
type SubChannelMetric struct {
	ID          int64
	RefName     string
	ChannelData *ChannelInternalMetric
	NestedChans map[int64]string
	SubChans    map[int64]string
	Sockets     map[int64]string
}

// ChannelInternalMetric defines the metrics an implementor of Channel interface
// should report.
type ChannelInternalMetric struct {
	State                    connectivity.State
	Target                   string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

// Channel is the interface that should be satisfied in order to be tracked by
// channelz as a channel type (i.e TopChannelT, SubChannelT and NestedChannelT).
type Channel interface {
	ChannelzMetric() *ChannelInternalMetric
}

type channel struct {
	Name        string
	c           Channel
	closeCalled bool
	nestedChans map[int64]string
	subChans    map[int64]string
	id          int64
	pid         int64
	cm          *channelMap
}

func (c *channel) addChild(id int64, e entry) {
	switch v := e.(type) {
	case *subChannel:
		c.subChans[id] = v.Name
	case *channel:
		c.nestedChans[id] = v.Name
	default:
		grpclog.Errorf("cannot add a child of type %T to a channel", e)
	}
}

func (c *channel) deleteChild(id int64) {
	delete(c.subChans, id)
	delete(c.nestedChans, id)
	c.canDelete()
}

func (c *channel) delete() {
	// channel has already been deleted
	if c == nil {
		return
	}
	c.closeCalled = true
	if len(c.subChans)+len(c.nestedChans) != 0 {
		return
	}
	c.cm.deleteEntry(c.id)
	if c.pid != 0 {
		c.cm.findEntry(c.pid).deleteChild(c.id)
	}
}

func (c *channel) canDelete() {
	if c.closeCalled && len(c.subChans)+len(c.nestedChans) == 0 {
		c.cm.deleteEntry(c.id)
		if c.pid != 0 {
			c.cm.findEntry(c.pid).deleteChild(c.id)
		}
	}
	return
}

type subChannel struct {
	Name        string
	c           Channel
	closeCalled bool
	sockets     map[int64]string
	id          int64
	pid         int64
	cm          *channelMap
}

func (sc *subChannel) addChild(id int64, e entry) {
	if v, ok := e.(*normalSocket); ok {
		sc.sockets[id] = v.Name
	} else {
		grpclog.Errorf("cannot add a child of type %T to a subChannel", e)
	}
}

func (sc *subChannel) deleteChild(id int64) {
	delete(sc.sockets, id)
	sc.canDelete()
}

func (sc *subChannel) delete() {
	// subChannel has already been deleted
	if sc == nil {
		return
	}
	sc.closeCalled = true
	if len(sc.sockets) != 0 {
		return
	}
	sc.cm.deleteEntry(sc.id)
	sc.cm.findEntry(sc.pid).deleteChild(sc.id)
}

func (sc *subChannel) canDelete() {
	if sc.closeCalled && len(sc.sockets) == 0 {
		sc.cm.deleteEntry(sc.id)
		sc.cm.findEntry(sc.pid).deleteChild(sc.id)
	}
	return
}

// SocketMetric defines the info provided by channelz for a specific Socket, which
// includes SocketInternalMetric and channelz-specific data, i.e, Channelz ID, etc.
type SocketMetric struct {
	ID         int64
	RefName    string
	SocketData *SocketInternalMetric
}

// SocketInternalMetric defines the metrics an implementor of Socket interface
// should report.
type SocketInternalMetric struct {
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
	ChannelzMetric() *SocketInternalMetric
}

type listenSocket struct {
	Name string
	s    Socket
	id   int64
	pid  int64
	cm   *channelMap
}

func (ls *listenSocket) addChild(id int64, e entry) {
	grpclog.Errorf("cannot add a child of type %T to a listen socket", e)
}

func (ls *listenSocket) deleteChild(id int64) {}

func (ls *listenSocket) delete() {
	// listenSocket has already been deleted
	if ls == nil {
		return
	}
	ls.cm.deleteEntry(ls.id)
	if v := ls.cm.findEntry(ls.pid); v != nil {
		v.deleteChild(ls.id)
	}
}

func (ls *listenSocket) canDelete() {}

type normalSocket struct {
	Name string
	s    Socket
	id   int64
	pid  int64
	cm   *channelMap
}

func (ns *normalSocket) addChild(id int64, e entry) {
	grpclog.Errorf("cannot add a child of type %T to a normal socket", e)
}

func (ns *normalSocket) deleteChild(id int64) {}

func (ns *normalSocket) delete() {
	// normalSocket has already been deleted
	if ns == nil {
		return
	}
	ns.cm.deleteEntry(ns.id)
	if v := ns.cm.findEntry(ns.pid); v != nil {
		v.deleteChild(ns.id)
	}
}

func (ns *normalSocket) canDelete() {}

// ServerMetric defines the info provided by channelz for a specific Server, which
// includes ServerInternalMetric and channelz-specific data, i.e, Channelz ID, etc.
type ServerMetric struct {
	ID            int64
	RefName       string
	ServerData    *ServerInternalMetric
	ListenSockets map[int64]string
}

// ServerInternalMetric defines the metrics collected for a Server.
type ServerInternalMetric struct {
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

// Server is the interface implemented by grpc.Server that reports ServerInternalMetric.
type Server interface {
	ChannelzMetric() *ServerInternalMetric
}

type server struct {
	Name          string
	s             Server
	closeCalled   bool
	sockets       map[int64]string
	listenSockets map[int64]string
	id            int64
	cm            *channelMap
}

func (s *server) addChild(id int64, e entry) {
	switch v := e.(type) {
	case *normalSocket:
		s.sockets[id] = v.Name
	case *listenSocket:
		s.listenSockets[id] = v.Name
	default:
		grpclog.Errorf("cannot add a child of type %T to a server", e)
	}
}

func (s *server) deleteChild(id int64) {
	delete(s.sockets, id)
	delete(s.listenSockets, id)
	s.canDelete()
}

func (s *server) delete() {
	// server has already been deleted
	if s == nil {
		return
	}
	s.closeCalled = true
	if len(s.sockets)+len(s.listenSockets) != 0 {
		return
	}
	s.cm.deleteEntry(s.id)
}

func (s *server) canDelete() {
	if s.closeCalled && len(s.sockets)+len(s.listenSockets) == 0 {
		s.cm.deleteEntry(s.id)
	}
	return
}
