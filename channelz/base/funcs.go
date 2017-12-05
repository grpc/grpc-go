package channelz

import (
	"sync"
	"sync/atomic"

	"google.golang.org/grpc/grpclog"
)

var (
	db    DB
	idGen idGenerator
)

// NewChannelzStorage initializes channelz data storage and unique id generator,
// and returns pointer to the data storage for read-only access.
func NewChannelzStorage() DB {
	db = &channelMap{
		m:                make(map[int64]conn),
		topLevelChannels: make(map[int64]struct{}),
		servers:          make(map[int64]struct{}),
	}
	idGen = idGenerator{}
	return db
}

// DB is the interface that groups the methods for channelz data storage and access
type DB interface {
	Database
	internal
}

// Database manages read-only access to channelz storage.
type Database interface {
	GetTopChannels(id int64) ([]int64, bool)
	GetServers(id int64) ([]int64, bool)
	GetServerSockets(id int64) ([]int64, bool)
	GetChannel(id int64) ChannelMetric
	GetSubChannel(id int64) ChannelMetric
	GetSocket(id int64) SocketMetric
	GetServer(id int64) ServerMetric
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

type internal interface {
	add(id int64, cn conn)
	addTopChannel(id int64, cn conn)
	addServer(id int64, cn conn)
	deleteEntry(id int64)
	addChild(pid, cid int64, ref string)
}

// RegisterChannel registers the given channel in db as ChannelType t.
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
		db.addTopChannel(id, cn)
	case SubChannelType:
		cn.t = subChannelT
		db.add(id, cn)
	case NestedChannelType:
		cn.t = nestedChannelT
		db.add(id, cn)
	default:
		// code should never reach here
	}
	return id
}

// RegisterServer registers the given server in db.
func RegisterServer(s Server) int64 {
	id := idGen.genID()
	db.addServer(id, &server{s: s, children: make(map[int64]string)})
	return id
}

// RegisterSocket registers the given socket in db.
func RegisterSocket(s Socket) int64 {
	id := idGen.genID()
	s.SetID(id)
	db.add(id, &socket{s: s})
	return id
}

// RemoveEntry removes an entry with unique identification number id from db.
func RemoveEntry(id int64) {
	db.deleteEntry(id)
}

// AddChild adds a child to its parent's descendant set with ref as its reference
// string, and set pid as its parent id.
func AddChild(pid, cid int64, ref string) {
	db.addChild(pid, cid, ref)
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
		case socketT:
			delete(p.(*channel).sockets, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
		}
	case serverT:
		switch child.Type() {
		case socketT:
			delete(p.(*server).children, cid)
		default:
			grpclog.Error("%+v cannot have a child of type %+v", p.Type(), child.Type())
		}
	case socketT:
		grpclog.Error("socket cannot have child")
	}
}

//TODO: optimize channelMap access here
func (c *channelMap) deleteEntry(id int64) {
	c.mu.Lock()
	if _, ok := c.topLevelChannels[id]; ok {
		delete(c.topLevelChannels, id)
	}

	if _, ok := c.servers[id]; ok {
		delete(c.servers, id)
	}

	if v, ok := c.m[id]; ok {
		switch v.Type() {
		case socketT:
			if v.(*socket).pid != 0 {
				c.removeChild(v.(*socket).pid, id)
			}
		case subChannelT, nestedChannelT:
			if v.(*channel).pid != 0 {
				c.removeChild(v.(*channel).pid, id)
			}
		}
	}
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
		case topChannelT, serverT:
			grpclog.Errorf("%+v cannot be the child of another entity", child.Type())
		case subChannelT:
			p.(*channel).subChans[cid] = ref
			child.(*channel).pid = pid
		case nestedChannelT:
			p.(*channel).nestedChans[cid] = ref
			child.(*channel).pid = pid
		case socketT:
			p.(*channel).sockets[cid] = ref
			child.(*socket).pid = pid
		}
	case serverT:
		switch child.Type() {
		case topChannelT, subChannelT, nestedChannelT, serverT:
			grpclog.Errorf("%+v cannot be the child of a server", child.Type())
		case socketT:
			p.(*server).children[cid] = ref
			child.(*socket).pid = pid
		}
	case socketT:
		grpclog.Errorf("socket cannot have children, id: %d", pid)
		return
	}
}

func (c *channelMap) GetTopChannels(id int64) ([]int64, bool) {
	return []int64{}, false
}

func (c *channelMap) GetServers(id int64) ([]int64, bool) {
	return []int64{}, false
}

func (c *channelMap) GetServerSockets(id int64) ([]int64, bool) {
	return []int64{}, false
}

func (c *channelMap) GetChannel(id int64) ChannelMetric {
	return ChannelMetric{}
}

func (c *channelMap) GetSubChannel(id int64) ChannelMetric {
	return ChannelMetric{}
}

func (c *channelMap) GetSocket(id int64) SocketMetric {
	return SocketMetric{}
}

func (c *channelMap) GetServer(id int64) ServerMetric {
	return ServerMetric{}
}

type idGenerator struct {
	id int64
}

func (i *idGenerator) genID() int64 {
	return atomic.AddInt64(&i.id, 1)
}
