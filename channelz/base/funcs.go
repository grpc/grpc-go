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
	"sort"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/grpclog"
)

var (
	db    dbWrapper
	idGen idGenerator
	// EntryPerPage defines the number of channelz entries shown on a web page.
	EntryPerPage = 50
	curState     int32
)

// TurnOn turns on channelz data collection.
// This is an EXPERIMENTAL API.
func TurnOn() {
	atomic.StoreInt32(&curState, 1)
}

// IsOn returns whether channelz data collection is on.
// This is an EXPERIMENTAL API.
func IsOn() bool {
	return atomic.LoadInt32(&curState) == 1
}

type dbWrapper struct {
	mu sync.RWMutex
	DB
}

func (d *dbWrapper) set(db DB) {
	d.mu.Lock()
	d.DB = db
	d.mu.Unlock()
}

func (d *dbWrapper) get() DB {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.DB
}

// NewChannelzStorage initializes channelz data storage and unique id generator,
// and returns pointer to the data storage for read-only access.
// This is an EXPERIMENTAL API.
func NewChannelzStorage() DB {
	db.set(&channelMap{
		m:                make(map[int64]conn),
		topLevelChannels: make(map[int64]struct{}),
		servers:          make(map[int64]struct{}),
	})
	idGen.reset()
	return &db
}

// DB is the interface that groups the methods for channelz data storage and access
type DB interface {
	Database
	internal
}

// Database manages read-only access to channelz storage.
type Database interface {
	GetTopChannels(id int64) ([]*ChannelMetric, bool)
	GetServers(id int64) ([]*ServerMetric, bool)
	GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool)
	GetChannel(id int64) *ChannelMetric
	GetSubChannel(id int64) *ChannelMetric
	GetSocket(id int64) *SocketMetric
}

// ChannelType defines three types of channel, they are TopChannelType, SubChannelType,
// and NestedChannelType.
type ChannelType int

const (
	// TopChannelType is the type of channel which is the root.
	TopChannelType ChannelType = iota
	// SubChannelType is the type of channel which is load balanced over.
	SubChannelType
	// NestedChannelType is the type of channel which is neither the root nor load balanced over,
	// e.g. ClientConn used in grpclb.
	NestedChannelType
)

// SocketType defines two types of socket, they are NormalSocketType and ListenSocketType.
type SocketType int

const (
	// NormalSocketType is the type of socket that is used for transmitting RPC.
	NormalSocketType SocketType = iota
	// ListenSocketType is the type of socket that is used for listening for incoming
	// connection on server side.
	ListenSocketType
)

type internal interface {
	add(id int64, cn conn)
	addTopChannel(id int64, cn conn)
	addServer(id int64, cn conn)
	deleteEntry(id int64)
	addChild(pid, cid int64, ref string)
}

// RegisterChannel registers the given channel in db as ChannelType t.
// This is an EXPERIMENTAL API.
func RegisterChannel(c Channel, t ChannelType) int64 {
	id := idGen.genID()
	cn := &channel{
		c:           c,
		subChans:    make(map[int64]string),
		nestedChans: make(map[int64]string),
		sockets:     make(map[int64]string),
	}
	switch t {
	case TopChannelType:
		cn.t = topChannelT
		db.get().addTopChannel(id, cn)
	case SubChannelType:
		cn.t = subChannelT
		db.get().add(id, cn)
	case NestedChannelType:
		cn.t = nestedChannelT
		db.get().add(id, cn)
	default:
		grpclog.Errorf("register channel with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}
	return id
}

// RegisterServer registers the given server in db.
// This is an EXPERIMENTAL API.
func RegisterServer(s Server) int64 {
	id := idGen.genID()
	db.get().addServer(id, &server{s: s, sockets: make(map[int64]string), listenSockets: make(map[int64]string)})
	return id
}

// RegisterSocket registers the given socket in db.
// This is an EXPERIMENTAL API.
func RegisterSocket(s Socket, t SocketType) int64 {
	id := idGen.genID()
	sk := &socket{s: s}
	switch t {
	case NormalSocketType:
		sk.t = normalSocketT
		db.get().add(id, sk)
	case ListenSocketType:
		sk.t = listenSocketT
		db.get().add(id, sk)
	default:
		grpclog.Errorf("register socket with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}
	return id
}

// RemoveEntry removes an entry with unique identification number id from db.
// This is an EXPERIMENTAL API.
func RemoveEntry(id int64) {
	db.get().deleteEntry(id)
}

// AddChild adds a child to its parent's descendant set with ref as its reference
// string, and set pid as its parent id.
// This is an EXPERIMENTAL API.
func AddChild(pid, cid int64, ref string) {
	db.get().addChild(pid, cid, ref)
}

type channelMap struct {
	mu               sync.RWMutex
	m                map[int64]conn
	topLevelChannels map[int64]struct{}
	servers          map[int64]struct{}
}

func (c *channelMap) add(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.mu.Unlock()
}

func (c *channelMap) addTopChannel(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.topLevelChannels[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) addServer(id int64, cn conn) {
	c.mu.Lock()
	c.m[id] = cn
	c.servers[id] = struct{}{}
	c.mu.Unlock()
}

// removeChild must be called where channelMap is already held.
func (c *channelMap) removeChild(pid, cid int64) {
	p, ok := c.m[pid]
	if !ok {
		grpclog.Info("parent has been deleted.")
		return
	}
	child, ok := c.m[cid]
	if !ok {
		grpclog.Info("child has been deleted.")
		return
	}
	switch p.Type() {
	case topChannelT, subChannelT, nestedChannelT:
		switch child.Type() {
		case subChannelT:
			delete(p.(*channel).subChans, cid)
		case nestedChannelT:
			delete(p.(*channel).nestedChans, cid)
		case normalSocketT:
			delete(p.(*channel).sockets, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
		}
	case serverT:
		switch child.Type() {
		case normalSocketT:
			delete(p.(*server).sockets, cid)
		case listenSocketT:
			delete(p.(*server).listenSockets, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
		}
	case normalSocketT, listenSocketT:
		grpclog.Error("socket cannot have child")
	}
}

//TODO: optimize channelMap access here
func (c *channelMap) deleteEntry(id int64) {
	c.mu.Lock()
	// Delete itself from topLevelChannels if it is there.
	if _, ok := c.topLevelChannels[id]; ok {
		delete(c.topLevelChannels, id)
	}
	// Delete itself from servers if it is there.
	if _, ok := c.servers[id]; ok {
		delete(c.servers, id)
	}

	// Delete itself from the descendants map of its parent.
	if v, ok := c.m[id]; ok {
		switch v.Type() {
		case normalSocketT, listenSocketT:
			if v.(*socket).pid != 0 {
				c.removeChild(v.(*socket).pid, id)
			}
		case subChannelT, nestedChannelT:
			if v.(*channel).pid != 0 {
				c.removeChild(v.(*channel).pid, id)
			}
		}
	}

	// Delete itself from the map
	delete(c.m, id)
	c.mu.Unlock()
}

//TODO: optimize channelMap access here
func (c *channelMap) addChild(pid, cid int64, ref string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.m[pid]
	if !ok {
		grpclog.Infof("parent has been deleted, id %d", pid)
		return
	}
	child, ok := c.m[cid]
	if !ok {
		grpclog.Infof("child has been deleted, id %d", cid)
		return
	}
	switch p.Type() {
	case topChannelT, subChannelT, nestedChannelT:
		switch child.Type() {
		case topChannelT, serverT, listenSocketT:
			grpclog.Errorf("%+v cannot be the child of a channel", child.Type())
		case subChannelT:
			p.(*channel).subChans[cid] = ref
			child.(*channel).pid = pid
		case nestedChannelT:
			p.(*channel).nestedChans[cid] = ref
			child.(*channel).pid = pid
		case normalSocketT:
			p.(*channel).sockets[cid] = ref
			child.(*socket).pid = pid
		}
	case serverT:
		switch child.Type() {
		case topChannelT, subChannelT, nestedChannelT, serverT:
			grpclog.Errorf("%+v cannot be the child of a server", child.Type())
		case normalSocketT:
			p.(*server).sockets[cid] = ref
			child.(*socket).pid = pid
		case listenSocketT:
			p.(*server).listenSockets[cid] = ref
			child.(*socket).pid = pid
		}
	case listenSocketT, normalSocketT:
		grpclog.Errorf("socket cannot have children, id: %d", pid)
		return
	}
}

type int64Slice []int64

func (s int64Slice) Len() int           { return len(s) }
func (s int64Slice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s int64Slice) Less(i, j int) bool { return s[i] < s[j] }

func copyMap(m map[int64]string) map[int64]string {
	n := make(map[int64]string)
	for k, v := range m {
		n[k] = v
	}
	return n
}

func (c *channelMap) GetTopChannels(id int64) ([]*ChannelMetric, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var t []*ChannelMetric
	ids := make([]int64, 0, len(c.topLevelChannels))
	for k := range c.topLevelChannels {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	count := 0
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < id {
			continue
		}
		if cn, ok := c.m[v]; ok {
			cm := cn.(*channel).c.ChannelzMetrics()
			cm.NestedChans = copyMap(cn.(*channel).nestedChans)
			cm.SubChans = copyMap(cn.(*channel).subChans)
			cm.Sockets = copyMap(cn.(*channel).sockets)
			t = append(t, cm)
			count++
		}
		if i == len(ids)-1 {
			return t, true
		}
	}
	if count == 0 {
		return t, true
	}
	return t, false
}

func (c *channelMap) GetServers(id int64) ([]*ServerMetric, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s []*ServerMetric
	ids := make([]int64, 0, len(c.servers))
	for k := range c.servers {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	count := 0
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < id {
			continue
		}
		if cn, ok := c.m[v]; ok {
			cm := cn.(*server).s.ChannelzMetrics()
			cm.ListenSockets = copyMap(cn.(*server).listenSockets)
			s = append(s, cm)
			count++
		}
		if i == len(ids)-1 {
			return s, true
		}
	}
	if count == 0 {
		return s, true
	}
	return s, false
}

func (c *channelMap) GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var s []*SocketMetric
	var cn conn
	var ok bool
	if cn, ok = c.m[id]; !ok || cn.Type() != serverT {
		// server with id doesn't exist.
		return nil, true
	}
	sm := cn.(*server).sockets
	ids := make([]int64, 0, len(sm))
	for k := range sm {
		ids = append(ids, k)
	}
	sort.Sort((int64Slice(ids)))
	count := 0
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < startID {
			continue
		}
		if cn, ok := c.m[v]; ok {
			cm := cn.(*socket).s.ChannelzMetrics()
			s = append(s, cm)
			count++
		}
		if i == len(ids)-1 {
			return s, true
		}
	}
	if count == 0 {
		return s, true
	}
	return s, false
}

func (c *channelMap) GetChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var cn conn
	var ok bool
	if cn, ok = c.m[id]; !ok || cn.Type() != topChannelT || cn.Type() != nestedChannelT {
		// channel with id doesn't exist.
		return nil
	}
	cm := cn.(*channel).c.ChannelzMetrics()
	cm.NestedChans = copyMap(cn.(*channel).nestedChans)
	cm.SubChans = copyMap(cn.(*channel).subChans)
	cm.Sockets = copyMap(cn.(*channel).sockets)
	return cm
}

func (c *channelMap) GetSubChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var cn conn
	var ok bool
	if cn, ok = c.m[id]; !ok || cn.Type() != subChannelT {
		// subchannel with id doesn't exist.
		return nil
	}
	cm := cn.(*channel).c.ChannelzMetrics()
	cm.NestedChans = copyMap(cn.(*channel).nestedChans)
	cm.SubChans = copyMap(cn.(*channel).subChans)
	cm.Sockets = copyMap(cn.(*channel).sockets)
	return cm
}

func (c *channelMap) GetSocket(id int64) *SocketMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var cn conn
	var ok bool
	if cn, ok = c.m[id]; !ok || cn.Type() != normalSocketT || cn.Type() != listenSocketT {
		// socket with id doesn't exist.
		return nil
	}
	return cn.(*socket).s.ChannelzMetrics()
}

type idGenerator struct {
	id int64
}

func (i *idGenerator) reset() {
	atomic.StoreInt64(&i.id, 0)
}

func (i *idGenerator) genID() int64 {
	return atomic.AddInt64(&i.id, 1)
}
