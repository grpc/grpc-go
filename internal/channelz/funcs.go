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

// Package channelz defines internal APIs for enabling channelz service, entry
// registration/deletion, and accessing channelz data. It also defines channelz
// metric struct formats.
package channelz

import (
	"errors"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/internal"
)

var (
	// IDGen is the global channelz entity ID generator.  It should not be used
	// outside this package except by tests.
	IDGen IDGenerator

	db *channelMap = newChannelMap()
	// EntriesPerPage defines the number of channelz entries to be shown on a web page.
	EntriesPerPage = int64(50)
	curState       int32
)

// TurnOn turns on channelz data collection.
func TurnOn() {
	atomic.StoreInt32(&curState, 1)
}

func init() {
	internal.ChannelzTurnOffForTesting = func() {
		atomic.StoreInt32(&curState, 0)
	}
}

// IsOn returns whether channelz data collection is on.
func IsOn() bool {
	return atomic.LoadInt32(&curState) == 1
}

// GetTopChannels returns a slice of top channel's ChannelMetric, along with a
// boolean indicating whether there's more top channels to be queried for.
//
// The arg id specifies that only top channel with id at or above it will be
// included in the result. The returned slice is up to a length of the arg
// maxResults or EntriesPerPage if maxResults is zero, and is sorted in ascending
// id order.
func GetTopChannels(id int64, maxResults int64) ([]*ChannelMetric, bool) {
	return db.GetTopChannels(id, maxResults)
}

// GetServers returns a slice of server's ServerMetric, along with a
// boolean indicating whether there's more servers to be queried for.
//
// The arg id specifies that only server with id at or above it will be included
// in the result. The returned slice is up to a length of the arg maxResults or
// EntriesPerPage if maxResults is zero, and is sorted in ascending id order.
func GetServers(id int64, maxResults int64) ([]*ServerMetric, bool) {
	return db.GetServers(id, maxResults)
}

// GetServerSockets returns a slice of server's (identified by id) normal socket's
// SocketMetric, along with a boolean indicating whether there's more sockets to
// be queried for.
//
// The arg startID specifies that only sockets with id at or above it will be
// included in the result. The returned slice is up to a length of the arg maxResults
// or EntriesPerPage if maxResults is zero, and is sorted in ascending id order.
func GetServerSockets(id int64, startID int64, maxResults int64) ([]*SocketMetric, bool) {
	return db.GetServerSockets(id, startID, maxResults)
}

// GetChannel returns the ChannelMetric for the channel (identified by id).
func GetChannel(id int64) *ChannelMetric {
	return db.GetChannel(id)
}

// GetSubChannel returns the SubChannelMetric for the subchannel (identified by id).
func GetSubChannel(id int64) *SubChannelMetric {
	return db.GetSubChannel(id)
}

// GetSocket returns the SocketInternalMetric for the socket (identified by id).
func GetSocket(id int64) *SocketMetric {
	return db.GetSocket(id)
}

// GetServer returns the ServerMetric for the server (identified by id).
func GetServer(id int64) *ServerMetric {
	return db.GetServer(id)
}

// RegisterChannel registers the given channel c in the channelz database with
// ref as its reference name, and adds it to the child list of its parent
// (identified by pid). pid == nil means no parent.
//
// Returns a unique channelz identifier assigned to this channel.
//
// If channelz is not turned ON, the channelz database is not mutated.
func RegisterChannel(c Channel, pid *Identifier, ref string) *Identifier {
	id := IDGen.genID()
	var parent int64
	isTopChannel := true
	if pid != nil {
		isTopChannel = false
		parent = pid.Int()
	}

	if !IsOn() {
		return newIdentifer(RefChannel, id, pid)
	}

	cn := &channel{
		refName:     ref,
		c:           c,
		subChans:    make(map[int64]string),
		nestedChans: make(map[int64]string),
		id:          id,
		pid:         parent,
		trace:       &channelTrace{createdTime: time.Now(), events: make([]*TraceEvent, 0, getMaxTraceEntry())},
	}
	db.addChannel(id, cn, isTopChannel, parent)
	return newIdentifer(RefChannel, id, pid)
}

// RegisterSubChannel registers the given subChannel c in the channelz database
// with ref as its reference name, and adds it to the child list of its parent
// (identified by pid).
//
// Returns a unique channelz identifier assigned to this subChannel.
//
// If channelz is not turned ON, the channelz database is not mutated.
func RegisterSubChannel(c Channel, pid *Identifier, ref string) (*Identifier, error) {
	if pid == nil {
		return nil, errors.New("a SubChannel's parent id cannot be nil")
	}
	id := IDGen.genID()
	if !IsOn() {
		return newIdentifer(RefSubChannel, id, pid), nil
	}

	sc := &subChannel{
		refName: ref,
		c:       c,
		sockets: make(map[int64]string),
		id:      id,
		pid:     pid.Int(),
		trace:   &channelTrace{createdTime: time.Now(), events: make([]*TraceEvent, 0, getMaxTraceEntry())},
	}
	db.addSubChannel(id, sc, pid.Int())
	return newIdentifer(RefSubChannel, id, pid), nil
}

// RegisterServer registers the given server s in channelz database. It returns
// the unique channelz tracking id assigned to this server.
//
// If channelz is not turned ON, the channelz database is not mutated.
func RegisterServer(s Server, ref string) *Identifier {
	id := IDGen.genID()
	if !IsOn() {
		return newIdentifer(RefServer, id, nil)
	}

	svr := &server{
		refName:       ref,
		s:             s,
		sockets:       make(map[int64]string),
		listenSockets: make(map[int64]string),
		id:            id,
	}
	db.addServer(id, svr)
	return newIdentifer(RefServer, id, nil)
}

// RegisterListenSocket registers the given listen socket s in channelz database
// with ref as its reference name, and add it to the child list of its parent
// (identified by pid). It returns the unique channelz tracking id assigned to
// this listen socket.
//
// If channelz is not turned ON, the channelz database is not mutated.
func RegisterListenSocket(s Socket, pid *Identifier, ref string) (*Identifier, error) {
	if pid == nil {
		return nil, errors.New("a ListenSocket's parent id cannot be 0")
	}
	id := IDGen.genID()
	if !IsOn() {
		return newIdentifer(RefListenSocket, id, pid), nil
	}

	ls := &listenSocket{refName: ref, s: s, id: id, pid: pid.Int()}
	db.addListenSocket(id, ls, pid.Int())
	return newIdentifer(RefListenSocket, id, pid), nil
}

// RegisterNormalSocket registers the given normal socket s in channelz database
// with ref as its reference name, and adds it to the child list of its parent
// (identified by pid). It returns the unique channelz tracking id assigned to
// this normal socket.
//
// If channelz is not turned ON, the channelz database is not mutated.
func RegisterNormalSocket(s Socket, pid *Identifier, ref string) (*Identifier, error) {
	if pid == nil {
		return nil, errors.New("a NormalSocket's parent id cannot be 0")
	}
	id := IDGen.genID()
	if !IsOn() {
		return newIdentifer(RefNormalSocket, id, pid), nil
	}

	ns := &normalSocket{refName: ref, s: s, id: id, pid: pid.Int()}
	db.addNormalSocket(id, ns, pid.Int())
	return newIdentifer(RefNormalSocket, id, pid), nil
}

// RemoveEntry removes an entry with unique channelz tracking id to be id from
// channelz database.
//
// If channelz is not turned ON, this function is a no-op.
func RemoveEntry(id *Identifier) {
	if !IsOn() {
		return
	}
	db.removeEntry(id.Int())
}

// IDGenerator is an incrementing atomic that tracks IDs for channelz entities.
type IDGenerator struct {
	id int64
}

// Reset resets the generated ID back to zero.  Should only be used at
// initialization or by tests sensitive to the ID number.
func (i *IDGenerator) Reset() {
	atomic.StoreInt64(&i.id, 0)
}

func (i *IDGenerator) genID() int64 {
	return atomic.AddInt64(&i.id, 1)
}
