package monitoring

import (
	"google.golang.org/grpc/codes"
)

type RpcType string

const (
	Unary     RpcType = "unary"
	Streaming RpcType = "streaming"
)

// RpcMonitor is a per-RPC datastructure.
type PerRpcMonitor interface {
	// ReceivedMessage is called on every stream message received by the monitor.
	ReceivedMessage()

	// SentMessage is called on every stream message sent by the monitor.
	SentMessage()

	// Handled is called whenever the RPC handling completes (with OK or AppError).
	Handled(code codes.Code, description string)

	// Erred is called whenever the RPC failed due to RPC-layer errors.
	Erred(err error)
}

// ServerMonitor allocates new per-RPC monitors on the server side.
type RpcMonitor interface {
	// NewForRpc allocates a new per-RPC monitor, also signifying the start of an RPC call.
	NewForRpc(rpcType RpcType, fullMethod string) PerRpcMonitor
}

// NoOpMonitor is both a Client- and Server-side RpcMonitor that does nothing, for no allocations.
type NoOpMonitor struct{}

func (m *NoOpMonitor) NewForRpc(RpcType, string) PerRpcMonitor {
	return m
}

func (*NoOpMonitor) ReceivedMessage() {}

func (*NoOpMonitor) SentMessage() {}

func (*NoOpMonitor) Handled(code codes.Code, description string) {}

func (*NoOpMonitor) Erred(err error) {}
