/*
 *
 * Copyright 2014 gRPC authors.
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

// Package peer defines various peer information associated with RPCs and
// corresponding utils.
package peer

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/grpc/credentials"
)

// Peer contains the information of the peer for an RPC, such as the address
// and authentication information.
type Peer struct {
	// Addr is the peer address.
	Addr net.Addr
	// LocalAddr is the local address.
	LocalAddr net.Addr
	// AuthInfo is the authentication information of the transport.
	// It is nil if there is no transport security being used.
	AuthInfo credentials.AuthInfo
}

// String ensures the Peer types implements the Stringer interface in order to
// allow to print a context with a peerKey value effectively.
func (p *Peer) String() string {
	if p == nil {
		return "Peer<nil>"
	}
	ret := "Peer{"
	if p.Addr != nil {
		ret += fmt.Sprintf("Addr: '%s'", p.Addr.String())
	}
	if p.AuthInfo != nil {
		if len(ret) > 5 {
			ret += ", "
		}
		ret += fmt.Sprintf("AuthInfo: '%s'", p.AuthInfo.AuthType())
	}
	ret += "}"
	return ret
}

type peerKey struct{}

// NewContext creates a new context with peer information attached.
func NewContext(ctx context.Context, p *Peer) context.Context {
	return context.WithValue(ctx, peerKey{}, p)
}

// FromContext returns the peer information in ctx if it exists.
func FromContext(ctx context.Context) (p *Peer, ok bool) {
	p, ok = ctx.Value(peerKey{}).(*Peer)
	return
}
