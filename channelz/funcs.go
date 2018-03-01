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
	if !IsOn() {
		NewChannelzStorage()
		atomic.StoreInt32(&curState, 1)
	}
}

// IsOn returns whether channelz data collection is on.
// This is an EXPERIMENTAL API.
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

// NewChannelzStorage initializes channelz data storage and unique id generator.
// Note: For testing purpose only. User should not call it in most cases.
// This is an EXPERIMENTAL API.
func NewChannelzStorage() {
	db.set(&channelMap{
		entries:          make(map[int64]entry),
		topLevelChannels: make(map[int64]struct{}),
		servers:          make(map[int64]struct{}),
	})
	idGen.reset()
}

// GetTopChannels returns up to EntryPerPage number of top channel metric whose
// identification number is larger than or equal to id, along with a boolean
// indicating whether there is more top channels to be queried.
// This is an EXPERIMENTAL API.
func GetTopChannels(id int64) ([]*ChannelMetric, bool) {
	return db.get().GetTopChannels(id)
}

// GetServers returns up to EntryPerPage number of server metric whose
// identification number is larger than or equal to id, along with a boolean
// indicating whether there is more servers to be queried.
// This is an EXPERIMENTAL API.
func GetServers(id int64) ([]*ServerMetric, bool) {
	return db.get().GetServers(id)
}

// GetServerSockets returns up to EntryPerPage number of SocketMetric who is a
// child of server with identification number to be id and whose identification
// number is larger than startID, along with a boolean indicating whether there
// is more sockets to be queried.
// This is an EXPERIMENTAL API.
func GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool) {
	return db.get().GetServerSockets(id, startID)
}

// GetChannel returns the ChannelMetric for the channel with identification number
// to be id.
// This is an EXPERIMENTAL API.
func GetChannel(id int64) *ChannelMetric {
	return db.get().GetChannel(id)
}

// GetSubChannel returns the ChannelMetric for the sub channel with identification
// number to be id.
// This is an EXPERIMENTAL API.
func GetSubChannel(id int64) *ChannelMetric {
	return db.get().GetSubChannel(id)
}

// GetSocket returns the SocketMetric for the socket with identification number
// to be id.
// This is an EXPERIMENTAL API.
func GetSocket(id int64) *SocketMetric {
	return db.get().GetSocket(id)
}

// RegisterChannel registers the given channel in db as EntryType t, with ref
// as its reference name, and sets parent ID to be pid. zero-value pid means no
// parent.
// This is an EXPERIMENTAL API.
func RegisterChannel(c Channel, t EntryType, pid int64, ref string) int64 {
	id := idGen.genID()
	cn := &channel{
		c:           c,
		subChans:    make(map[int64]string),
		nestedChans: make(map[int64]string),
		sockets:     make(map[int64]string),
	}
	switch t {
	case TopChannelT:
		cn.t = TopChannelT
		db.get().addTopChannel(id, cn)
	case SubChannelT:
		cn.t = SubChannelT
		db.get().addNonRootEntry(id, cn)
	case NestedChannelT:
		cn.t = NestedChannelT
		db.get().addNonRootEntry(id, cn)
	default:
		grpclog.Errorf("register channel with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}

	if pid != 0 {
		db.get().addChildToParent(pid, id, ref)
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

// RegisterSocket registers the given socket in db as EntryType t, with ref as
// its reference name, and sets pid as its parent ID.
// This is an EXPERIMENTAL API.
func RegisterSocket(s Socket, t EntryType, pid int64, ref string) int64 {
	id := idGen.genID()
	sk := &socket{s: s}
	switch t {
	case NormalSocketT:
		sk.t = NormalSocketT
		db.get().addNonRootEntry(id, sk)
	case ListenSocketT:
		sk.t = ListenSocketT
		db.get().addNonRootEntry(id, sk)
	default:
		grpclog.Errorf("register socket with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}

	if pid != 0 {
		db.get().addChildToParent(pid, id, ref)
	}
	return id
}

// RemoveEntry removes an entry with unique identification number id from db.
// This is an EXPERIMENTAL API.
func RemoveEntry(id int64) {
	db.get().removeEntry(id)
}

// channelMap is the storage data structure for channelz.
type channelMap struct {
	mu               sync.RWMutex
	entries          map[int64]entry
	topLevelChannels map[int64]struct{}
	servers          map[int64]struct{}
}

func (c *channelMap) addNonRootEntry(id int64, cn entry) {
	c.mu.Lock()
	c.entries[id] = cn
	c.mu.Unlock()
}

func (c *channelMap) addTopChannel(id int64, cn entry) {
	c.mu.Lock()
	c.entries[id] = cn
	c.topLevelChannels[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) addServer(id int64, cn entry) {
	c.mu.Lock()
	c.entries[id] = cn
	c.servers[id] = struct{}{}
	c.mu.Unlock()
}

func (c *channelMap) removeEntry(id int64) {
	c.mu.Lock()
	c.recursivelyDeleteEntry(id)
	c.mu.Unlock()
}

// recursivelyDeleteEntry should be called inside c.mu lock.
func (c *channelMap) markCloseCalled(cn entry) {
	switch cn.Type() {
	case TopChannelT, SubChannelT, NestedChannelT:
		cn.(*channel).closeCalled = true
	case ServerT:
		cn.(*server).closeCalled = true
	}
}

// readyDelete should be called inside c.mu lock.
func (c *channelMap) readyDelete(cn entry) bool {
	switch cn.Type() {
	case NormalSocketT, ListenSocketT:
		return true
	case TopChannelT, SubChannelT, NestedChannelT:
		if len(cn.(*channel).subChans)+len(cn.(*channel).nestedChans)+len(cn.(*channel).sockets) == 0 {
			return true
		}
	case ServerT:
		// TODO: should we take listenSockets into accounts?
		if len(cn.(*server).sockets)+len(cn.(*server).listenSockets) == 0 {
			return true
		}
	}
	return false
}

// removeChildFromParent must be called where lock on channelMap is already held.
func (c *channelMap) removeChildFromParent(pid, cid int64) {
	p, ok := c.entries[pid]
	if !ok {
		grpclog.Infof("parent (id: %d) has been deleted.", pid)
		return
	}
	child, ok := c.entries[cid]
	if !ok {
		grpclog.Infof("child (id: %d), has been deleted.", cid)
		return
	}
	var closeCalledPreviously bool
	switch p.Type() {
	case TopChannelT, SubChannelT, NestedChannelT:
		closeCalledPreviously = p.(*channel).closeCalled
		switch child.Type() {
		case SubChannelT:
			delete(p.(*channel).subChans, cid)
		case NestedChannelT:
			delete(p.(*channel).nestedChans, cid)
		case NormalSocketT:
			delete(p.(*channel).sockets, cid)
		default:
			grpclog.Errorf("%+v cannot have a child of type %+v", p.Type(), child.Type())
			return
		}
	case ServerT:
		closeCalledPreviously = p.(*server).closeCalled
		switch child.Type() {
		case NormalSocketT:
			delete(p.(*server).sockets, cid)
		case ListenSocketT:
			delete(p.(*server).listenSockets, cid)
		default:
			grpclog.Errorf("%+v cannot have a child of type %+v", p.Type(), child.Type())
			return
		}
	case NormalSocketT, ListenSocketT:
		grpclog.Error("socket cannot have child")
		return
	}

	if closeCalledPreviously {
		c.recursivelyDeleteEntry(pid)
	}
}

// TODO: optimize channelMap access here
// recursivelyDeleteEntry should be called inside c.mu lock.
func (c *channelMap) recursivelyDeleteEntry(id int64) {
	v, ok := c.entries[id]
	if !ok {
		grpclog.Infof("entry with id: %d doesn't not exist", id)
		return
	}

	c.markCloseCalled(v)
	if !c.readyDelete(v) {
		return
	}

	// Delete itself from topLevelChannels if it is there.
	if _, ok := c.topLevelChannels[id]; ok {
		delete(c.topLevelChannels, id)
	}
	// Delete itself from servers if it is there.
	if _, ok := c.servers[id]; ok {
		delete(c.servers, id)
	}

	// Delete itself from the descendants map of its parent.
	switch v.Type() {
	case NormalSocketT, ListenSocketT:
		if v.(*socket).pid != 0 {
			c.removeChildFromParent(v.(*socket).pid, id)
		}
	case SubChannelT, NestedChannelT:
		if v.(*channel).pid != 0 {
			c.removeChildFromParent(v.(*channel).pid, id)
		}
	default:
		// TopChannel and Server cannot have parent
	}

	// Delete itself from the entries map
	delete(c.entries, id)
}

//TODO: optimize channelMap access here
func (c *channelMap) addChildToParent(pid, cid int64, ref string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.entries[pid]
	if !ok {
		grpclog.Warningf("parent has been deleted, pid %d, cid %d", pid, cid)
		return
	}
	child, ok := c.entries[cid]
	if !ok {
		grpclog.Warningf("child has been deleted, pid %d, cid %d", pid, cid)
		return
	}
	switch p.Type() {
	case TopChannelT, SubChannelT, NestedChannelT:
		switch child.Type() {
		case TopChannelT, ServerT, ListenSocketT:
			grpclog.Errorf("%+v cannot be the child of a channel", child.Type())
		case SubChannelT:
			p.(*channel).subChans[cid] = ref
			child.(*channel).pid = pid
		case NestedChannelT:
			p.(*channel).nestedChans[cid] = ref
			child.(*channel).pid = pid
		case NormalSocketT:
			p.(*channel).sockets[cid] = ref
			child.(*socket).pid = pid
		}
	case ServerT:
		switch child.Type() {
		case TopChannelT, SubChannelT, NestedChannelT, ServerT:
			grpclog.Errorf("%+v cannot be the child of a server", child.Type())
		case NormalSocketT:
			p.(*server).sockets[cid] = ref
			child.(*socket).pid = pid
		case ListenSocketT:
			p.(*server).listenSockets[cid] = ref
			child.(*socket).pid = pid
		}
	case ListenSocketT, NormalSocketT:
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (c *channelMap) GetTopChannels(id int64) ([]*ChannelMetric, bool) {
	var t []*ChannelMetric
	c.mu.RLock()
	l := len(c.topLevelChannels)
	ids := make([]int64, 0, l)
	cns := make([]entry, 0, min(l, EntryPerPage))

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
		if cn, ok := c.entries[v]; ok {
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

	for _, cn := range cns {
		cm := cn.(*channel).c.ChannelzMetric()
		c.mu.RLock()
		cm.NestedChans = copyMap(cn.(*channel).nestedChans)
		cm.SubChans = copyMap(cn.(*channel).subChans)
		cm.Sockets = copyMap(cn.(*channel).sockets)
		c.mu.RUnlock()
		t = append(t, cm)
	}
	return t, ok
}

func (c *channelMap) GetServers(id int64) ([]*ServerMetric, bool) {
	var s []*ServerMetric
	c.mu.RLock()
	l := len(c.servers)
	ids := make([]int64, 0, l)
	ss := make([]entry, 0, min(l, EntryPerPage))
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
		if cn, ok := c.entries[v]; ok {
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

	for _, cn := range ss {
		cm := cn.(*server).s.ChannelzMetric()
		c.mu.RLock()
		cm.ListenSockets = copyMap(cn.(*server).listenSockets)
		c.mu.RUnlock()
		s = append(s, cm)
	}
	return s, ok
}

func (c *channelMap) GetServerSockets(id int64, startID int64) ([]*SocketMetric, bool) {
	var s []*SocketMetric
	var cn entry
	var ok bool
	c.mu.RLock()
	if cn, ok = c.entries[id]; !ok || cn.Type() != ServerT {
		// server with id doesn't exist.
		c.mu.RUnlock()
		return nil, true
	}
	sm := cn.(*server).sockets
	l := len(sm)
	ids := make([]int64, 0, l)
	sks := make([]entry, 0, min(l, EntryPerPage))
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
		if cn, ok := c.entries[v]; ok {
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

	for _, cn := range sks {
		cm := cn.(*socket).s.ChannelzMetric()
		s = append(s, cm)
	}
	return s, ok
}

func (c *channelMap) GetChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	var cn entry
	var ok bool
	if cn, ok = c.entries[id]; !ok || (cn.Type() != TopChannelT && cn.Type() != NestedChannelT) {
		// channel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()
	cm := cn.(*channel).c.ChannelzMetric()
	c.mu.RLock()
	cm.NestedChans = copyMap(cn.(*channel).nestedChans)
	cm.SubChans = copyMap(cn.(*channel).subChans)
	cm.Sockets = copyMap(cn.(*channel).sockets)
	c.mu.RUnlock()
	return cm
}

func (c *channelMap) GetSubChannel(id int64) *ChannelMetric {
	c.mu.RLock()
	var cn entry
	var ok bool
	if cn, ok = c.entries[id]; !ok || cn.Type() != SubChannelT {
		// subchannel with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()
	cm := cn.(*channel).c.ChannelzMetric()
	c.mu.RLock()
	cm.NestedChans = copyMap(cn.(*channel).nestedChans)
	cm.SubChans = copyMap(cn.(*channel).subChans)
	cm.Sockets = copyMap(cn.(*channel).sockets)
	c.mu.RUnlock()
	return cm
}

func (c *channelMap) GetSocket(id int64) *SocketMetric {
	var cn entry
	var ok bool
	c.mu.RLock()
	if cn, ok = c.entries[id]; !ok || (cn.Type() != NormalSocketT && cn.Type() != ListenSocketT) {
		// socket with id doesn't exist.
		c.mu.RUnlock()
		return nil
	}
	return cn.(*socket).s.ChannelzMetric()
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
