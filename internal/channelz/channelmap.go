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
	"sort"
	"sync"
	"time"
)

// entry represents a node in the channelz database.
type entry interface {
	// addChild adds a child e, whose channelz id is id to child list
	addChild(id int64, e entry)
	// deleteChild deletes a child with channelz id to be id from child list
	deleteChild(id int64)
	// triggerDelete tries to delete self from channelz database. However, if child
	// list is not empty, then deletion from the database is on hold until the last
	// child is deleted from database.
	triggerDelete()
	// deleteSelfIfReady check whether triggerDelete() has been called before, and whether child
	// list is now empty. If both conditions are met, then delete self from database.
	deleteSelfIfReady()
	// getParentID returns parent ID of the entry. 0 value parent ID means no parent.
	getParentID() int64
}

// channelMap is the storage data structure for channelz.
// Methods of channelMap can be divided in two two categories with respect to locking.
// 1. Methods acquire the global lock.
// 2. Methods that can only be called when global lock is held.
// A second type of method need always to be called inside a first type of method.
type channelMap struct {
	mu               sync.RWMutex
	topLevelChannels map[int64]struct{}
	servers          map[int64]*server
	channels         map[int64]*channel
	subChannels      map[int64]*subChannel
	listenSockets    map[int64]*listenSocket
	normalSockets    map[int64]*normalSocket
}

func newChannelMap() *channelMap {
	return &channelMap{
		topLevelChannels: make(map[int64]struct{}),
		channels:         make(map[int64]*channel),
		listenSockets:    make(map[int64]*listenSocket),
		normalSockets:    make(map[int64]*normalSocket),
		servers:          make(map[int64]*server),
		subChannels:      make(map[int64]*subChannel),
	}
}

func (c *channelMap) addServer(id int64, s *server) {
	c.mu.Lock()
	s.cm = c
	c.servers[id] = s
	c.mu.Unlock()
}

func (c *channelMap) addChannel(id int64, cn *channel, isTopChannel bool, pid int64) {
	c.mu.Lock()
	cn.cm = c
	cn.trace.cm = c
	c.channels[id] = cn
	if isTopChannel {
		c.topLevelChannels[id] = struct{}{}
	} else {
		c.findEntry(pid).addChild(id, cn)
	}
	c.mu.Unlock()
}

func (c *channelMap) addSubChannel(id int64, sc *subChannel, pid int64) {
	c.mu.Lock()
	sc.cm = c
	sc.trace.cm = c
	c.subChannels[id] = sc
	c.findEntry(pid).addChild(id, sc)
	c.mu.Unlock()
}

func (c *channelMap) addListenSocket(id int64, ls *listenSocket, pid int64) {
	c.mu.Lock()
	ls.cm = c
	c.listenSockets[id] = ls
	c.findEntry(pid).addChild(id, ls)
	c.mu.Unlock()
}

func (c *channelMap) addNormalSocket(id int64, ns *normalSocket, pid int64) {
	c.mu.Lock()
	ns.cm = c
	c.normalSockets[id] = ns
	c.findEntry(pid).addChild(id, ns)
	c.mu.Unlock()
}

// removeEntry triggers the removal of an entry, which may not indeed delete the entry, if it has to
// wait on the deletion of its children and until no other entity's channel trace references it.
// It may lead to a chain of entry deletion. For example, deleting the last socket of a gracefully
// shutting down server will lead to the server being also deleted.
func (c *channelMap) removeEntry(id int64) {
	c.mu.Lock()
	c.findEntry(id).triggerDelete()
	c.mu.Unlock()
}

// tracedChannel represents tracing operations which are present on both
// channels and subChannels.
type tracedChannel interface {
	getChannelTrace() *channelTrace
	incrTraceRefCount()
	decrTraceRefCount()
	getRefName() string
}

// c.mu must be held by the caller
func (c *channelMap) decrTraceRefCount(id int64) {
	e := c.findEntry(id)
	if v, ok := e.(tracedChannel); ok {
		v.decrTraceRefCount()
		e.deleteSelfIfReady()
	}
}

// c.mu must be held by the caller.
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

// c.mu must be held by the caller
// deleteEntry simply deletes an entry from the channelMap. Before calling this
// method, caller must check this entry is ready to be deleted, i.e removeEntry()
// has been called on it, and no children still exist.
// Conditionals are ordered by the expected frequency of deletion of each entity
// type, in order to optimize performance.
func (c *channelMap) deleteEntry(id int64) {
	var ok bool
	if _, ok = c.normalSockets[id]; ok {
		delete(c.normalSockets, id)
		return
	}
	if _, ok = c.subChannels[id]; ok {
		delete(c.subChannels, id)
		return
	}
	if _, ok = c.channels[id]; ok {
		delete(c.channels, id)
		delete(c.topLevelChannels, id)
		return
	}
	if _, ok = c.listenSockets[id]; ok {
		delete(c.listenSockets, id)
		return
	}
	if _, ok = c.servers[id]; ok {
		delete(c.servers, id)
		return
	}
}

func (c *channelMap) traceEvent(id int64, desc *TraceEventDesc) {
	c.mu.Lock()
	child := c.findEntry(id)
	childTC, ok := child.(tracedChannel)
	if !ok {
		c.mu.Unlock()
		return
	}
	childTC.getChannelTrace().append(&TraceEvent{Desc: desc.Desc, Severity: desc.Severity, Timestamp: time.Now()})
	if desc.Parent != nil {
		parent := c.findEntry(child.getParentID())
		var chanType RefChannelType
		switch child.(type) {
		case *channel:
			chanType = RefChannel
		case *subChannel:
			chanType = RefSubChannel
		}
		if parentTC, ok := parent.(tracedChannel); ok {
			parentTC.getChannelTrace().append(&TraceEvent{
				Desc:      desc.Parent.Desc,
				Severity:  desc.Parent.Severity,
				Timestamp: time.Now(),
				RefID:     id,
				RefName:   childTC.getRefName(),
				RefType:   chanType,
			})
			childTC.incrTraceRefCount()
		}
	}
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

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (c *channelMap) GetTopChannels(id int64, maxResults int64) ([]*ChannelMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	c.mu.RLock()
	l := int64(len(c.topLevelChannels))
	ids := make([]int64, 0, l)
	cns := make([]*channel, 0, min(l, maxResults))

	for k := range c.topLevelChannels {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
	count := int64(0)
	var end bool
	var t []*ChannelMetric
	for i, v := range ids[idx:] {
		if count == maxResults {
			break
		}
		if cn, ok := c.channels[v]; ok {
			cns = append(cns, cn)
			t = append(t, &ChannelMetric{
				NestedChans: copyMap(cn.nestedChans),
				SubChans:    copyMap(cn.subChans),
			})
			count++
		}
		if i == len(ids[idx:])-1 {
			end = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		end = true
	}

	for i, cn := range cns {
		t[i].ChannelData = cn.c.ChannelzMetric()
		t[i].ID = cn.id
		t[i].RefName = cn.refName
		t[i].Trace = cn.trace.dumpData()
	}
	return t, end
}

func (c *channelMap) GetServers(id, maxResults int64) ([]*ServerMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	c.mu.RLock()
	l := int64(len(c.servers))
	ids := make([]int64, 0, l)
	ss := make([]*server, 0, min(l, maxResults))
	for k := range c.servers {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
	count := int64(0)
	var end bool
	var s []*ServerMetric
	for i, v := range ids[idx:] {
		if count == maxResults {
			break
		}
		if svr, ok := c.servers[v]; ok {
			ss = append(ss, svr)
			s = append(s, &ServerMetric{
				ListenSockets: copyMap(svr.listenSockets),
			})
			count++
		}
		if i == len(ids[idx:])-1 {
			end = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		end = true
	}

	for i, svr := range ss {
		s[i].ServerData = svr.s.ChannelzMetric()
		s[i].ID = svr.id
		s[i].RefName = svr.refName
	}
	return s, end
}

func (c *channelMap) GetServerSockets(id int64, startID int64, maxResults int64) ([]*SocketMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	var svr *server
	var ok bool
	c.mu.RLock()
	if svr, ok = c.servers[id]; !ok {
		// server with id doesn't exist.
		c.mu.RUnlock()
		return nil, true
	}
	svrskts := svr.sockets
	l := int64(len(svrskts))
	ids := make([]int64, 0, l)
	sks := make([]*normalSocket, 0, min(l, maxResults))
	for k := range svrskts {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= startID })
	count := int64(0)
	var end bool
	for i, v := range ids[idx:] {
		if count == maxResults {
			break
		}
		if ns, ok := c.normalSockets[v]; ok {
			sks = append(sks, ns)
			count++
		}
		if i == len(ids[idx:])-1 {
			end = true
			break
		}
	}
	c.mu.RUnlock()
	if count == 0 {
		end = true
	}
	s := make([]*SocketMetric, 0, len(sks))
	for _, ns := range sks {
		sm := &SocketMetric{}
		sm.SocketData = ns.s.ChannelzMetric()
		sm.ID = ns.id
		sm.RefName = ns.refName
		s = append(s, sm)
	}
	return s, end
}

func (c *channelMap) GetChannel(id int64) *ChannelMetric {
	cm := &ChannelMetric{}
	var cn *channel
	var ok bool
	c.mu.RLock()
	if cn, ok = c.channels[id]; !ok {
		// channel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	cm.NestedChans = copyMap(cn.nestedChans)
	cm.SubChans = copyMap(cn.subChans)
	// cn.c can be set to &dummyChannel{} when deleteSelfFromMap is called. Save a copy of cn.c when
	// holding the lock to prevent potential data race.
	chanCopy := cn.c
	c.mu.RUnlock()
	cm.ChannelData = chanCopy.ChannelzMetric()
	cm.ID = cn.id
	cm.RefName = cn.refName
	cm.Trace = cn.trace.dumpData()
	return cm
}

func (c *channelMap) GetSubChannel(id int64) *SubChannelMetric {
	cm := &SubChannelMetric{}
	var sc *subChannel
	var ok bool
	c.mu.RLock()
	if sc, ok = c.subChannels[id]; !ok {
		// subchannel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	cm.Sockets = copyMap(sc.sockets)
	// sc.c can be set to &dummyChannel{} when deleteSelfFromMap is called. Save a copy of sc.c when
	// holding the lock to prevent potential data race.
	chanCopy := sc.c
	c.mu.RUnlock()
	cm.ChannelData = chanCopy.ChannelzMetric()
	cm.ID = sc.id
	cm.RefName = sc.refName
	cm.Trace = sc.trace.dumpData()
	return cm
}

func (c *channelMap) GetSocket(id int64) *SocketMetric {
	sm := &SocketMetric{}
	c.mu.RLock()
	if ls, ok := c.listenSockets[id]; ok {
		c.mu.RUnlock()
		sm.SocketData = ls.s.ChannelzMetric()
		sm.ID = ls.id
		sm.RefName = ls.refName
		return sm
	}
	if ns, ok := c.normalSockets[id]; ok {
		c.mu.RUnlock()
		sm.SocketData = ns.s.ChannelzMetric()
		sm.ID = ns.id
		sm.RefName = ns.refName
		return sm
	}
	c.mu.RUnlock()
	return nil
}

func (c *channelMap) GetServer(id int64) *ServerMetric {
	sm := &ServerMetric{}
	var svr *server
	var ok bool
	c.mu.RLock()
	if svr, ok = c.servers[id]; !ok {
		c.mu.RUnlock()
		return nil
	}
	sm.ListenSockets = copyMap(svr.listenSockets)
	c.mu.RUnlock()
	sm.ID = svr.id
	sm.RefName = svr.refName
	sm.ServerData = svr.s.ChannelzMetric()
	return sm
}

// dummyEntry is a fake entry to handle entry not found case.
type dummyEntry struct {
	idNotFound int64
}

func (d *dummyEntry) addChild(id int64, e entry) {
	// Note: It is possible for a normal program to reach here under race condition.
	// For example, there could be a race between ClientConn.Close() info being propagated
	// to addrConn and http2Client. ClientConn.Close() cancel the context and result
	// in http2Client to error. The error info is then caught by transport monitor
	// and before addrConn.tearDown() is called in side ClientConn.Close(). Therefore,
	// the addrConn will create a new transport. And when registering the new transport in
	// channelz, its parent addrConn could have already been torn down and deleted
	// from channelz tracking, and thus reach the code here.
	logger.Infof("attempt to add child of type %T with id %d to a parent (id=%d) that doesn't currently exist", e, id, d.idNotFound)
}

func (d *dummyEntry) deleteChild(id int64) {
	// It is possible for a normal program to reach here under race condition.
	// Refer to the example described in addChild().
	logger.Infof("attempt to delete child with id %d from a parent (id=%d) that doesn't currently exist", id, d.idNotFound)
}

func (d *dummyEntry) triggerDelete() {
	logger.Warningf("attempt to delete an entry (id=%d) that doesn't currently exist", d.idNotFound)
}

func (*dummyEntry) deleteSelfIfReady() {
	// code should not reach here. deleteSelfIfReady is always called on an existing entry.
}

func (*dummyEntry) getParentID() int64 {
	return 0
}
