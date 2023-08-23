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
	defer c.mu.Unlock()
	s.cm = c
	c.servers[id] = s
}

func (c *channelMap) addChannel(id int64, cn *channel, isTopChannel bool, pid int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cn.cm = c
	cn.trace.cm = c
	c.channels[id] = cn
	if isTopChannel {
		c.topLevelChannels[id] = struct{}{}
	} else {
		c.findEntry(pid).addChild(id, cn)
	}
}

func (c *channelMap) addSubChannel(id int64, sc *subChannel, pid int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sc.cm = c
	sc.trace.cm = c
	c.subChannels[id] = sc
	c.findEntry(pid).addChild(id, sc)
}

func (c *channelMap) addListenSocket(id int64, ls *listenSocket, pid int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ls.cm = c
	c.listenSockets[id] = ls
	c.findEntry(pid).addChild(id, ls)
}

func (c *channelMap) addNormalSocket(id int64, ns *normalSocket, pid int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ns.cm = c
	c.normalSockets[id] = ns
	c.findEntry(pid).addChild(id, ns)
}

// removeEntry triggers the removal of an entry, which may not indeed delete the
// entry, if it has to wait on the deletion of its children and until no other
// entity's channel trace references it.  It may lead to a chain of entry
// deletion. For example, deleting the last socket of a gracefully shutting down
// server will lead to the server being also deleted.
func (c *channelMap) removeEntry(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.findEntry(id).triggerDelete()
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
	if v, ok := c.channels[id]; ok {
		return v
	}
	if v, ok := c.subChannels[id]; ok {
		return v
	}
	if v, ok := c.servers[id]; ok {
		return v
	}
	if v, ok := c.listenSockets[id]; ok {
		return v
	}
	if v, ok := c.normalSockets[id]; ok {
		return v
	}
	return &dummyEntry{idNotFound: id}
}

// c.mu must be held by the caller
//
// deleteEntry deletes an entry from the channelMap. Before calling this method,
// caller must check this entry is ready to be deleted, i.e removeEntry() has
// been called on it, and no children still exist.
func (c *channelMap) deleteEntry(id int64) {
	if _, ok := c.normalSockets[id]; ok {
		delete(c.normalSockets, id)
		return
	}
	if _, ok := c.subChannels[id]; ok {
		delete(c.subChannels, id)
		return
	}
	if _, ok := c.channels[id]; ok {
		delete(c.channels, id)
		delete(c.topLevelChannels, id)
		return
	}
	if _, ok := c.listenSockets[id]; ok {
		delete(c.listenSockets, id)
		return
	}
	if _, ok := c.servers[id]; ok {
		delete(c.servers, id)
		return
	}
}

func (c *channelMap) traceEvent(id int64, desc *TraceEventDesc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	child := c.findEntry(id)
	childTC, ok := child.(tracedChannel)
	if !ok {
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

func (c *channelMap) GetTopChannels(id int64, maxResults int) ([]*ChannelMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	l := int64(len(c.topLevelChannels))
	ids := make([]int64, 0, l)

	for k := range c.topLevelChannels {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
	end := true
	var t []*ChannelMetric
	for _, v := range ids[idx:] {
		if len(t) == maxResults {
			end = false
			break
		}
		if cn, ok := c.channels[v]; ok {
			t = append(t, &ChannelMetric{
				ID:          cn.id,
				RefName:     cn.refName,
				ChannelData: cn.metrics,
				NestedChans: copyMap(cn.nestedChans),
				SubChans:    copyMap(cn.subChans),
				Trace:       cn.trace.dumpData(),
			})
		}
	}
	return t, end
}

func (c *channelMap) GetServers(id int64, maxResults int) ([]*ServerMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]int64, 0, len(c.servers))
	for k := range c.servers {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= id })
	end := true
	var s []*ServerMetric
	for _, v := range ids[idx:] {
		if len(s) == maxResults {
			end = false
			break
		}
		if svr, ok := c.servers[v]; ok {
			s = append(s, &ServerMetric{
				ListenSockets: copyMap(svr.listenSockets),
				ServerData:    svr.s.ChannelzMetric(),
				ID:            svr.id,
				RefName:       svr.refName,
			})
		}
	}
	return s, end
}

func (c *channelMap) GetServerSockets(id int64, startID int64, maxResults int) ([]*SocketMetric, bool) {
	if maxResults <= 0 {
		maxResults = EntriesPerPage
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	svr, ok := c.servers[id]
	if !ok {
		// server with id doesn't exist.
		return nil, true
	}
	svrskts := svr.sockets
	ids := make([]int64, 0, len(svrskts))
	sks := make([]*SocketMetric, 0, min(len(svrskts), maxResults))
	for k := range svrskts {
		ids = append(ids, k)
	}
	sort.Sort(int64Slice(ids))
	idx := sort.Search(len(ids), func(i int) bool { return ids[i] >= startID })
	end := true
	for _, v := range ids[idx:] {
		if len(sks) == maxResults {
			end = false
			break
		}
		if ns, ok := c.normalSockets[v]; ok {
			sks = append(sks, &SocketMetric{
				SocketData: ns.s.ChannelzMetric(),
				ID:         ns.id,
				RefName:    ns.refName,
			})
		}
	}
	return sks, end
}

func (c *channelMap) GetChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cn, ok := c.channels[id]
	if !ok {
		// channel with id doesn't exist.
		return nil
	}
	return &ChannelMetric{
		ID:          cn.id,
		RefName:     cn.refName,
		ChannelData: cn.metrics,
		NestedChans: copyMap(cn.nestedChans),
		SubChans:    copyMap(cn.subChans),
		Trace:       cn.trace.dumpData(),
	}
}

func (c *channelMap) GetSubChannel(id int64) *SubChannelMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	sc, ok := c.subChannels[id]
	if !ok {
		// subchannel with id doesn't exist.
		return nil
	}
	return &SubChannelMetric{
		Sockets:     copyMap(sc.sockets),
		ChannelData: sc.metrics,
		ID:          sc.id,
		RefName:     sc.refName,
		Trace:       sc.trace.dumpData(),
	}
}

func (c *channelMap) GetSocket(id int64) *SocketMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ls, ok := c.listenSockets[id]; ok {
		return &SocketMetric{
			SocketData: ls.s.ChannelzMetric(),
			ID:         ls.id,
			RefName:    ls.refName,
		}
	}
	if ns, ok := c.normalSockets[id]; ok {
		return &SocketMetric{
			SocketData: ns.s.ChannelzMetric(),
			ID:         ns.id,
			RefName:    ns.refName,
		}
	}
	return nil
}

func (c *channelMap) GetServer(id int64) *ServerMetric {
	sm := &ServerMetric{}
	var svr *server
	var ok bool
	c.mu.RLock()
	defer c.mu.RUnlock()
	if svr, ok = c.servers[id]; !ok {
		return nil
	}
	sm.ListenSockets = copyMap(svr.listenSockets)
	sm.ID = svr.id
	sm.RefName = svr.refName
	sm.ServerData = svr.s.ChannelzMetric()
	return sm
}

type dummyEntry struct {
	// dummyEntry is a fake entry to handle entry not found case.
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
