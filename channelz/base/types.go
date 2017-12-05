package channelz

import (
	"net"
	"sync"
	"time"

	"google.golang.org/grpc/connectivity"
)

type entryType int

const (
	topChannelT entryType = iota
	subChannelT
	nestedChannelT
	serverT
	socketT
)

type conn interface {
	Type() entryType
	sync.Locker
}

// ChannelMetric defines the metrics collected for a Channel.
type ChannelMetric struct {
	RefName                  string
	State                    connectivity.State
	Target                   string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

// Channel is the interface implemented by grpc.ClientConn and grpc.addrConn
// that reports ChannelMetric.
type Channel interface {
	ChannelzMetrics() ChannelMetric
}

type channel struct {
	t           entryType
	c           Channel
	mu          sync.Mutex
	nestedChans map[int64]string
	subChans    map[int64]string
	sockets     map[int64]string
	pid         int64
}

func (c *channel) Type() entryType {
	return c.t
}

func (c *channel) Lock() {
	c.mu.Lock()
}

func (c *channel) Unlock() {
	c.mu.Unlock()
}

// SocketMetric defines the metrics collected for a Socket.
type SocketMetric struct {
	RefName                          string
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
	ChannelzMetrics() SocketMetric
	IncrMsgSent()
	IncrMsgRecv()
	SetID(int64)
}

type socket struct {
	name string
	s    Socket
	pid  int64
}

func (*socket) Type() entryType {
	return socketT
}

func (s *socket) Lock() {
}

func (s *socket) Unlock() {
}

// ServerMetric defines the metrics collected for a Server.
type ServerMetric struct {
	RefName                  string
	CallsStarted             int64
	CallsSucceeded           int64
	CallsFailed              int64
	LastCallStartedTimestamp time.Time
	// trace
}

// Server is the interface implemented by grpc.Server that reports ServerMetric.
type Server interface {
	ChannelzMetrics() ServerMetric
}

type server struct {
	name     string
	s        Server
	mu       sync.Mutex
	children map[int64]string
}

func (*server) Type() entryType {
	return serverT
}

func (s *server) Lock() {
	s.mu.Lock()
}

func (s *server) Unlock() {
	s.mu.Unlock()
}
