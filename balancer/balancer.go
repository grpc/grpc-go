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

// Package balancer defines APIs for load balancing in gRPC.
// All APIs in this package are experimental.
package balancer

import (
	"errors"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/resolver"
)

var (
	// m is a map from name to balancer builder.
	m map[string]Builder
	// defaultName is the default balancer to use.
	defaultName string
)

func init() {
	// TODO(bar) install pickfirst.
	m = make(map[string]Builder)
}

// Register registers the balancer builder to the balancer map.
// b.Name will be used as the name registered with this builder.
func Register(b Builder) {
	m[b.Name()] = b
}

// Get returns the resolver builder registered with the given name.
// If no builder is register with the name, the default pickfirst will
// be used.
func Get(name string) (b Builder, ok bool) {
	b, ok = m[name]
	if ok {
		return
	}
	b, ok = m[defaultName]
	return
}

// SubConnection represents a gRPC sub connection.
// Each sub connection contains a list of addresses. gRPC will
// try to connect to them (in sequence), and stop trying the
// remainings if one connection was successful.
//
// The reconnect backoff will be applied on the list, not a single address.
// For example, try_on_all_addresses -> backoff -> try_on_all_addresses.
//
// All SubConnection starts in IDLE, and will not try to connect. To trigger
// the connecting, Balancers must call Connect.
// When the connection encounters an error, it will reconnect immediately.
// When the connection becomes IDLE, it will not reconnect unless Connect is
// called.
type SubConnection interface {
	// UpdateAddresses updates the addresses used in this SubConnection.
	// gRPC checks if the address of connection in use is still in the new list.
	// If it's in the list, the connection will be kept.
	// If it's not in the list, the connection will gracefully closed, and
	// a new connection will be created.
	UpdateAddresses([]resolver.Address)
	// Connect starts the connecting for this SubConnection.
	Connect()
}

// NewSubConnectionOptions contains options to create new SubConnection.
type NewSubConnectionOptions struct{}

// ClientConnection represents a gRPC ClientConn.
type ClientConnection interface {
	// NewSubConnection is called by balancer to create a new SubConnection.
	// It doesn't block and wait for the connections to be established.
	// Behaviors of the SubConnection can be controlled by options.
	NewSubConnection([]resolver.Address, NewSubConnectionOptions) (SubConnection, error)
	// RemoveSubConnection removes the SubConnection from ClientConn.
	// The SubConnection will be shutdown.
	RemoveSubConnection(SubConnection)

	// UpdateBalancerState is called by balancer to nofity gRPC that some internal
	// state in balancer has changed.
	//
	// gRPC will update the connectivity state of the ClientConn, and will call pick
	// on the new picker to pick new SubConnection.
	UpdateBalancerState(s connectivity.State, p Picker)

	// Target returns the dial target for this ClientConnection.
	Target() string
}

// BuildOptions contains additional information for Build.
type BuildOptions struct {
	// DialCreds is the transport credential the Balancer implementation can
	// use to dial to a remote load balancer server. The Balancer implementations
	// can ignore this if it does not need to talk to another party securely.
	DialCreds credentials.TransportCredentials
	// Dialer is the custom dialer the Balancer implementation can use to dial
	// to a remote load balancer server. The Balancer implementations
	// can ignore this if it doesn't need to talk to remote balancer.
	Dialer func(context.Context, string) (net.Conn, error)
}

// Builder creates a balancer.
type Builder interface {
	// Build creates a new balancer with the ClientConnection.
	Build(cc ClientConnection, opts BuildOptions) Balancer
	// Name returns the name of balancers built by this builder.
	// It will be used to pick balancers (for example in service config).
	Name() string
}

// PickOptions contains addition information for the Pick operation.
type PickOptions struct{}

// PutInfo contains additional information for Put.
type PutInfo struct {
	// Err is the rpc error the RPC finished with. It could be nil.
	Err error
}

// ErrNoSubConnAvailable indicates no SubConnection is available for pick().
// gRPC will block the RPC until a new picker is available via UpdateBalancerState().
var ErrNoSubConnAvailable = errors.New("no sub connection is available")

// Picker is used by gRPC to pick a SubConnection to send an RPC.
// Balancer is expected to generate a new picker from it's snapshot everytime it's
// internal state has changed.
//
// The pickers used by gRPC can be updated by UpdateBalancerState().
type Picker interface {
	// Pick returns the SubConnection to be used to send the RPC.
	// The returned SubConnection must be one returned by NewSubConnection().
	//
	// This functions is expected to return:
	// - a SubConnection that is known to be READY;
	// - ErrNoSubConnAvailable if no SubConnection is available, but progress is being
	//   made (for example, some SubConnection is in CONNECTING mode);
	// - other errors if no active connecting is happening (for example, all SubConnections
	//   are in TRANSIENT_FAILURE mode).
	//
	// If a SubConnection is returned:
	// - If it is READY, gRPC will send the RPC on it;
	// - If it is not ready, or becomes not ready after it's returned, gRPC will block
	//   this call until a new picker is updated and will call pick on the new picker.
	//
	// If the returned error is not nil:
	// - If the error is ErrNoSubConnAvailable, gRPC will block until UpdateBalancerState()
	// - If the error is not ErrNoSubConnAvailable:
	//   - If the RPC is non-failfast, gRPC will block until UpdateBalancerState()
	//     is called to pick again;
	//   - Otherwise, RPC is failed with unavailable error.
	//
	// The returned put() function will be called once the rpc has finished, with the
	// final status of that RPC.
	// It could be nil if balancer doesn't care about the RPC status.
	Pick(ctx context.Context, opts PickOptions) (conn SubConnection, put func(PutInfo), err error)
}

// Balancer takes the input from gRPC, manages SubConnections and collect and aggregate
// the connectivity states.
//
// It also generates and updates Picker to gRPC, which will be used to pick SubConnection
// for RPCs.
type Balancer interface {
	// HandleSubConnectionStateChange is called by gRPC when the connectivity state
	// of sc has changed.
	// Balancer is expected to aggregate all the state of SubConnections and report
	// that back to gRPC.
	// Balancer should also generate and update Pickers when its internal state has
	// been changed by the new state.
	HandleSubConnectionStateChange(sc SubConnection, state connectivity.State)
	// HandleResolvedResult is called by gRPC to send updated resolved addresses to
	// balancers.
	// Balancer can create new SubConnections or remove SubConnections with the addresses.
	// An empty address slice and a non-nil error will be passed if the resolver returns
	// non-nil error to gRPC.
	HandleResolvedResult([]resolver.Address, error)
	// Close closes the balancer.
	Close()
}
