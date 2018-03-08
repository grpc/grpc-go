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

// Package channelz defines APIs for enabling channelz service, entry
// registration/deletion, and accessing channelz data. It also defines channelz
// metric struct formats.
//
// All APIs in this package are experimental.
package channelz

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/grpclog"
)

var (
	db    dbWrapper
	idGen idGenerator
	// EntryPerPage defines the number of channelz entries to be shown on a web page.
	EntryPerPage = 50
	curState     int32
)

// TurnOn turns on channelz data collection.
func TurnOn() {
	if !IsOn() {
		NewChannelzStorage()
		atomic.StoreInt32(&curState, 1)
	}
}

// IsOn returns whether channelz data collection is on.
func IsOn() bool {
	return atomic.CompareAndSwapInt32(&curState, 1, 1)
}

// dbWarpper wraps around a reference to internal channelz data storage, and
// provide synchronized functionality to set and get the reference.
type dbWrapper struct {
	mu sync.RWMutex
	DB *channelMap
}

func (d *dbWrapper) set(db *channelMap) {
	d.mu.Lock()
	d.DB = db
	d.mu.Unlock()
}

func (d *dbWrapper) get() *channelMap {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.DB
}

// NewChannelzStorage initializes channelz data storage and id generator.
//
// Note: This function is exported for testing purpose only. User should not call
// it in most cases.
func NewChannelzStorage() {
	db.set(&channelMap{
		topLevelChannels: make(map[int64]struct{}),
		channels:         make(map[int64]*channel),
		listenSockets:    make(map[int64]*listenSocket),
		normalSockets:    make(map[int64]*normalSocket),
		servers:          make(map[int64]*server),
		subChannels:      make(map[int64]*subChannel),
	})
	idGen.reset()
}

// GetTopChannels returns a slice of top channel's ChannelMetric, along with a
// boolean indicating whether there's more top channels to be queried for.
//
// The arg id specifies that only top channel with id at or above it will be included
// in the result. The returned slice is up to a length of EntryPerPage, and is
// sorted in ascending id order.
func GetTopChannels(id int64) ([]*ChannelMetric, bool) {
	return db.get().GetTopChannels(id)
}

// GetServers returns a slice of server's ServerMetric, along with a
// boolean indicating whether there's more servers to be queried for.
//
// The arg id specifies that only server with id at or above it will be included
// in the result. The returned slice is up to a length of EntryPerPage, and is
// sorted in ascending id order.
func GetServers(id int64) ([]*ServerMetric, bool) {
	return db.get().GetServers(id)
}

// GetServerSockets returns a slice of server's (identified by id) normal socket's
// SocketMetric, along with a boolean indicating whether there's more sockets to
// be queried for.
//
// The arg startID specifies that only sockets with id at or above it will be
// included in the result. The returned slice is up to a length of EntryPerPage,
// and is sorted in ascending id order.
func GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool) {
	return db.get().GetServerSockets(id, startID)
}

// GetChannel returns the ChannelMetric for the channel (identified by id).
func GetChannel(id int64) *ChannelMetric {
	return db.get().GetChannel(id)
}

// GetSubChannel returns the SubChannelMetric for the subchannel (identified by id).
func GetSubChannel(id int64) *SubChannelMetric {
	return db.get().GetSubChannel(id)
}

// GetSocket returns the SocketInternalMetric for the socket (identified by id).
func GetSocket(id int64) *SocketMetric {
	return db.get().GetSocket(id)
}

// RegisterChannel registers the given channel c in channelz database with ref
// as its reference name, and add it to the child list of its parent (identified
// by pid). pid = 0 means no parent. It returns the unique channelz tracking id
// assigned to this channel.
func RegisterChannel(c Channel, pid int64, ref string) int64 {
	id := idGen.genID()
	cn := &channel{
		c:           c,
		subChans:    make(map[int64]string),
		nestedChans: make(map[int64]string),
		id:          id,
		pid:         pid,
	}
	if pid == 0 {
		db.get().addChannel(id, cn, true, pid, ref)
	} else {
		db.get().addChannel(id, cn, false, pid, ref)
	}
	return id
}

// RegisterSubChannel registers the given channel c in channelz database with ref
// as its reference name, and add it to the child list of its parent (identified
// by pid). It returns the unique channelz tracking id assigned to this subchannel.
func RegisterSubChannel(c Channel, pid int64, ref string) int64 {
	if pid == 0 {
		grpclog.Error("a SubChannel's parent id cannot be 0")
		return 0
	}
	id := idGen.genID()
	sc := &subChannel{
		c:       c,
		sockets: make(map[int64]string),
		id:      id,
		pid:     pid,
	}
	db.get().addSubChannel(id, sc, pid, ref)
	return id
}

// RegisterServer registers the given server s in channelz database. It returns
// the unique channelz tracking id assigned to this server.
func RegisterServer(s Server) int64 {
	id := idGen.genID()
	db.get().addServer(id, &server{s: s, sockets: make(map[int64]string), listenSockets: make(map[int64]string), id: id})
	return id
}

// RegisterListenSocket registers the given listen socket s in channelz database
// with ref as its reference name, and add it to the child list of its parent
// (identified by pid). It returns the unique channelz tracking id assigned to
// this listen socket.
func RegisterListenSocket(s Socket, pid int64, ref string) int64 {
	if pid == 0 {
		grpclog.Error("a ListenSocket's parent id cannot be 0")
		return 0
	}
	id := idGen.genID()
	ls := &listenSocket{s: s, id: id, pid: pid}
	db.get().addListenSocket(id, ls, pid, ref)
	return id
}

// RegisterNormalSocket registers the given normal socket s in channelz database
// with ref as its reference name, and add it to the child list of its parent
// (identified by pid). It returns the unique channelz tracking id assigned to
// this normal socket.
func RegisterNormalSocket(s Socket, pid int64, ref string) int64 {
	if pid == 0 {
		grpclog.Error("a NormalSocket's parent id cannot be 0")
		return 0
	}
	id := idGen.genID()
	ns := &normalSocket{s: s, id: id, pid: pid}
	db.get().addNormalSocket(id, ns, pid, ref)
	return id
}

// RemoveEntry removes an entry with unique channelz trakcing id to be id from
// channelz database.
func RemoveEntry(id int64) {
	db.get().removeEntry(id)
}

// channelMap is the storage data structure for channelz.
type channelMap struct {
	mu               sync.RWMutex
	topLevelChannels map[int64]struct{}
	servers          map[int64]*server
	channels         map[int64]*channel
	subChannels      map[int64]*subChannel
	listenSockets    map[int64]*listenSocket
	normalSockets    map[int64]*normalSocket
}

func (c *channelMap) addServer(id int64, s *server) {
	c.mu.Lock()
	s.cm = c
	c.servers[id] = s
	c.mu.Unlock()
}

func (c *channelMap) addChannel(id int64, cn *channel, isTopChannel bool, pid int64, ref string) {
	c.mu.Lock()
	cn.cm = c
	c.channels[id] = cn
	if isTopChannel {
		c.topLevelChannels[id] = struct{}{}
	} else {
		c.findEntry(pid).addChild(id, cn)
	}
	c.mu.Unlock()
}

func (c *channelMap) addSubChannel(id int64, sc *subChannel, pid int64, ref string) {
	c.mu.Lock()
	sc.cm = c
	c.subChannels[id] = sc
	c.findEntry(pid).addChild(id, sc)
	c.mu.Unlock()
}

func (c *channelMap) addListenSocket(id int64, ls *listenSocket, pid int64, ref string) {
	c.mu.Lock()
	ls.cm = c
	c.listenSockets[id] = ls
	c.findEntry(pid).addChild(id, ls)
	c.mu.Unlock()
}

func (c *channelMap) addNormalSocket(id int64, ns *normalSocket, pid int64, ref string) {
	c.mu.Lock()
	ns.cm = c
	c.normalSockets[id] = ns
	c.findEntry(pid).addChild(id, ns)
	c.mu.Unlock()
}

func (c *channelMap) findEntry(id int64) entry {
	var v entry
	var ok bool
	if v, ok = c.channels[id]; ok {
		return v
	}
	if v, ok = c.subChannels[id]; ok {
		return v
	}
	if v, ok = c.servers[id]; ok {
		return v
	}
	if v, ok = c.listenSockets[id]; ok {
		return v
	}
	if v, ok = c.normalSockets[id]; ok {
		return v
	}
	return &dummyEntry{idNotFound: id}
}

// PrintMap will be deleted before PR is merged.
func PrintMap() {
	c := db.get()
	c.mu.RLock()
	fmt.Println("toplevelchannels")
	fmt.Println(c.topLevelChannels)
	fmt.Println("len", len(c.topLevelChannels))
	fmt.Println("channels")
	fmt.Println(c.channels)
	fmt.Println("subchannels")
	fmt.Println(c.subChannels)
	fmt.Println("servers")
	fmt.Println(c.servers)
	fmt.Println("listensockets")
	fmt.Println(c.listenSockets)
	fmt.Println("normalsockets")
	fmt.Println(c.normalSockets)
	c.mu.RUnlock()
}

func (c *channelMap) deleteEntry(id int64) {
	var ok bool
	if _, ok = c.channels[id]; ok {
		delete(c.channels, id)
		delete(c.topLevelChannels, id)
		return
	}
	if _, ok = c.subChannels[id]; ok {
		delete(c.subChannels, id)
		return
	}
	if _, ok = c.servers[id]; ok {
		delete(c.servers, id)
		return
	}
	if _, ok = c.listenSockets[id]; ok {
		delete(c.listenSockets, id)
		return
	}
	if _, ok = c.normalSockets[id]; ok {
		delete(c.normalSockets, id)
		return
	}
}

func (c *channelMap) removeEntry(id int64) {
	c.mu.Lock()
	c.findEntry(id).delete()
	c.mu.Unlock()
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *channelMap) GetTopChannels(id int64) ([]*ChannelMetric, bool) {
	c.mu.RLock()
	l := len(c.topLevelChannels)
	ids := make([]int64, 0, l)
	cns := make([]*channel, 0, min(l, EntryPerPage))

	for k := range c.topLevelChannels {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	count := 0
	var ok bool
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < id {
			continue
		}
		if cn, ok := c.channels[v]; ok {
			cns = append(cns, cn)
			count++
		}
		if i == len(ids)-1 {
			ok = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		ok = true
	}

	var t []*ChannelMetric
	for _, cn := range cns {
		cim := cn.c.ChannelzMetric()
		cm := &ChannelMetric{}
		cm.ChannelData = cim
		c.mu.RLock()
		cm.NestedChans = copyMap(cn.nestedChans)
		cm.SubChans = copyMap(cn.subChans)
		c.mu.RUnlock()
		t = append(t, cm)
	}
	return t, ok
}

func (c *channelMap) GetServers(id int64) ([]*ServerMetric, bool) {
	c.mu.RLock()
	l := len(c.servers)
	ids := make([]int64, 0, l)
	ss := make([]*server, 0, min(l, EntryPerPage))
	for k := range c.servers {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	count := 0
	var ok bool
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < id {
			continue
		}
		if cn, ok := c.servers[v]; ok {
			ss = append(ss, cn)
			count++
		}
		if i == len(ids)-1 {
			ok = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		ok = true
	}

	var s []*ServerMetric
	for _, cn := range ss {
		sm := &ServerMetric{}
		sm.ServerData = cn.s.ChannelzMetric()
		sm.ID = cn.id
		sm.RefName = cn.refName
		c.mu.RLock()
		sm.ListenSockets = copyMap(cn.listenSockets)
		c.mu.RUnlock()
		s = append(s, sm)
	}
	return s, ok
}

func (c *channelMap) GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool) {
	var cn *server
	var ok bool
	c.mu.RLock()
	if cn, ok = c.servers[id]; !ok {
		// server with id doesn't exist.
		c.mu.RUnlock()
		return nil, true
	}
	sm := cn.sockets
	l := len(sm)
	ids := make([]int64, 0, l)
	sks := make([]*normalSocket, 0, min(l, EntryPerPage))
	for k := range sm {
		ids = append(ids, k)
	}
	sort.Sort((int64Slice(ids)))
	count := 0
	ok = false
	for i, v := range ids {
		if count == EntryPerPage {
			break
		}
		if v < startID {
			continue
		}
		if cn, ok := c.normalSockets[v]; ok {
			sks = append(sks, cn)
			count++
		}
		if i == len(ids)-1 {
			ok = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		ok = true
	}
	var s []*SocketMetric
	for _, cn := range sks {
		sm := &SocketMetric{}
		sm.SocketData = cn.s.ChannelzMetric()
		sm.ID = cn.id
		sm.RefName = cn.refName
		s = append(s, sm)
	}
	return s, ok
}

func (c *channelMap) GetChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	var cn *channel
	var ok bool
	if cn, ok = c.channels[id]; !ok {
		// channel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()
	cm := &ChannelMetric{}
	cm.ChannelData = cn.c.ChannelzMetric()
	cm.ID = cn.id
	cm.RefName = cn.refName
	c.mu.RLock()
	cm.NestedChans = copyMap(cn.nestedChans)
	cm.SubChans = copyMap(cn.subChans)
	c.mu.RUnlock()
	return cm
}

func (c *channelMap) GetSubChannel(id int64) *SubChannelMetric {
	c.mu.RLock()
	var cn *subChannel
	var ok bool
	if cn, ok = c.subChannels[id]; !ok {
		// subchannel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()
	cm := &SubChannelMetric{}
	cm.ChannelData = cn.c.ChannelzMetric()
	cm.ID = cn.id
	cm.RefName = cn.refName
	c.mu.RLock()
	cm.Sockets = copyMap(cn.sockets)
	c.mu.RUnlock()
	return cm
}

func (c *channelMap) GetSocket(id int64) *SocketMetric {
	sm := &SocketMetric{}
	c.mu.RLock()
	if cn, ok := c.listenSockets[id]; ok {
		c.mu.RUnlock()
		sm.SocketData = cn.s.ChannelzMetric()
		sm.ID = cn.id
		sm.RefName = cn.refName
		return sm
	}
	if cn, ok := c.normalSockets[id]; ok {
		c.mu.RUnlock()
		sm.SocketData = cn.s.ChannelzMetric()
		sm.ID = cn.id
		sm.RefName = cn.refName
		return sm
	}
	c.mu.RUnlock()
	return nil
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
