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
	"time"
)

// ServerMetric defines the info channelz provides for a specific Server, which
// includes ServerInternalMetric and channelz-specific data, such as channelz id,
// child list, etc.
type ServerMetric struct {
	// ID is the channelz id of this server.
	ID int64
	// RefName is the human readable reference string of this server.
	RefName string
	// ServerData contains server internal metric reported by the server through
	// ChannelzMetric().
	ServerData *ServerInternalMetric
	// ListenSockets tracks the listener socket type children of this server in the
	// format of a map from socket channelz id to corresponding reference string.
	ListenSockets map[int64]string
}

// ServerInternalMetric defines the struct that the implementor of Server interface
// should return from ChannelzMetric().
type ServerInternalMetric struct {
	// The number of incoming calls started on the server.
	CallsStarted int64
	// The number of incoming calls that have completed with an OK status.
	CallsSucceeded int64
	// The number of incoming calls that have a completed with a non-OK status.
	CallsFailed int64
	// The last time a call was started on the server.
	LastCallStartedTimestamp time.Time
}

// Server is the interface to be satisfied in order to be tracked by channelz as
// Server.
type Server interface {
	ChannelzMetric() *ServerInternalMetric
}

type server struct {
	refName       string
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
		s.sockets[id] = v.refName
	case *listenSocket:
		s.listenSockets[id] = v.refName
	default:
		logger.Errorf("cannot add a child (id = %d) of type %T to a server", id, e)
	}
}

func (s *server) deleteChild(id int64) {
	delete(s.sockets, id)
	delete(s.listenSockets, id)
	s.deleteSelfIfReady()
}

func (s *server) triggerDelete() {
	s.closeCalled = true
	s.deleteSelfIfReady()
}

func (s *server) deleteSelfIfReady() {
	if !s.closeCalled || len(s.sockets)+len(s.listenSockets) != 0 {
		return
	}
	s.cm.deleteEntry(s.id)
}

func (s *server) getParentID() int64 {
	return 0
}
