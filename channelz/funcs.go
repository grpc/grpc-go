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

	"golang.org/x/net/context"
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
	NewChannelzStorage()
	atomic.StoreInt32(&curState, 1)
}

// IsOn returns whether channelz data collection is on.
// This is an EXPERIMENTAL API.
func IsOn() bool {
	return atomic.CompareAndSwapInt32(&curState, 1, 1)
}

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

// NewChannelzStorage initializes channelz data storage and unique id generator,
// and returns pointer to the data storage for read-only access.
// This is an EXPERIMENTAL API.
func NewChannelzStorage() {
	db.set(&channelMap{
		m:                make(map[int64]conn),
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
		db.get().add(id, cn)
	case NestedChannelT:
		cn.t = NestedChannelT
		db.get().add(id, cn)
	default:
		grpclog.Errorf("register channel with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}

	if pid != 0 {
		db.get().addChild(pid, id, ref)
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
		db.get().add(id, sk)
	case ListenSocketT:
		sk.t = ListenSocketT
		db.get().add(id, sk)
	default:
		grpclog.Errorf("register socket with undefined type %+v", t)
		// Is returning 0 a good idea?
		return 0
	}

	if pid != 0 {
		db.get().addChild(pid, id, ref)
	}
	return id
}

// RemoveEntry removes an entry with unique identification number id from db.
// This is an EXPERIMENTAL API.
func RemoveEntry(id int64) {
	db.get().removeEntry(id)
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

func (c *channelMap) checkDelete(p conn, pid int64, t EntryType) {
	switch t {
	case TopChannelT, SubChannelT, NestedChannelT:
		if p.(*channel).closeCalled && len(p.(*channel).subChans)+len(p.(*channel).nestedChans)+len(p.(*channel).sockets) == 0 {
			c.deleteEntry(pid)
		}
	case ServerT:
		if p.(*server).closeCalled && len(p.(*server).sockets)+len(p.(*server).listenSockets) == 0 {
			c.deleteEntry(pid)
		}
	default:
		// code should not reach here
	}
}

// removeChild must be called where lock on channelMap is already held.
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
	case TopChannelT, SubChannelT, NestedChannelT:
		switch child.Type() {
		case SubChannelT:
			delete(p.(*channel).subChans, cid)
		case NestedChannelT:
			delete(p.(*channel).nestedChans, cid)
		case NormalSocketT:
			delete(p.(*channel).sockets, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
			return
		}
	case ServerT:
		switch child.Type() {
		case NormalSocketT:
			delete(p.(*server).sockets, cid)
		case ListenSocketT:
			delete(p.(*server).listenSockets, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
			return
		}
	case NormalSocketT, ListenSocketT:
		grpclog.Error("socket cannot have child")
		return
	}
	c.checkDelete(p, pid, p.Type())
}

// readyDelete should be called inside c.mu lock.
func (c *channelMap) readyDelete(id int64) bool {
	if v, ok := c.m[id]; ok {
		switch v.Type() {
		case NormalSocketT, ListenSocketT:
			return true
		case TopChannelT, SubChannelT, NestedChannelT:
			v.(*channel).Lock()
			defer v.(*channel).Unlock()
			if len(v.(*channel).subChans)+len(v.(*channel).nestedChans)+len(v.(*channel).sockets) > 0 {
				v.(*channel).closeCalled = true
				return false
			}
			return true
		case ServerT:
			v.(*server).Lock()
			defer v.(*server).Unlock()
			// TODO: should we take listenSockets into accounts?
			if len(v.(*server).sockets)+len(v.(*server).listenSockets) > 0 {
				v.(*server).closeCalled = true
				return false
			}
			return true
		}
	}
	return true
}

func (c *channelMap) removeEntry(id int64) {
	c.mu.Lock()
	c.deleteEntry(id)
	c.mu.Unlock()
}

//TODO: optimize channelMap access here
// deleteEntry should be called inside c.mu lock.
func (c *channelMap) deleteEntry(id int64) {
	if !c.readyDelete(id) {
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
	if v, ok := c.m[id]; ok {
		switch v.Type() {
		case NormalSocketT, ListenSocketT:
			if v.(*socket).pid != 0 {
				c.removeChild(v.(*socket).pid, id)
			}
		case SubChannelT, NestedChannelT:
			if v.(*channel).pid != 0 {
				c.removeChild(v.(*channel).pid, id)
			}
		}
	}

	// Delete itself from the map
	delete(c.m, id)
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
	if cn, ok = c.m[id]; !ok || cn.Type() != ServerT {
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
	if cn, ok = c.m[id]; !ok || cn.Type() != TopChannelT || cn.Type() != NestedChannelT {
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
	if cn, ok = c.m[id]; !ok || cn.Type() != SubChannelT {
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
	if cn, ok = c.m[id]; !ok || cn.Type() != NormalSocketT || cn.Type() != ListenSocketT {
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

// nestedChannel is the context key for indicating whether this ClientConn is a
// nested channel. Its corresponding value is the parent's ID.
type nestedChannel struct{}

// WithParentID stores the specified ID in the given context, and returns the new
// context.
func WithParentID(ctx context.Context, pid int64) context.Context {
	return context.WithValue(ctx, nestedChannel{}, pid)
}

// ParentID returns the ID stored in the given context, and returns 0 if it doesn't
// exist.
func ParentID(ctx context.Context) int64 {
	if v := ctx.Value(nestedChannel{}); v != nil {
		return v.(int64)
	}
	return 0
}
