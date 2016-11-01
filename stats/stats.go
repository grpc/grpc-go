/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

// Package stats reports stats for gRPC.
// This package is for monitoring purpose only.
// All APIs are experimental.
package stats // import "google.golang.org/grpc/stats"

import (
	"net"
	"sync/atomic"
	"time"

	"golang.org/x/net/context"
)

// Stats contains stats information about RPCs.
// All stats types in this package implements this interface.
type Stats interface {
	isStats()
}

// InitStats indicates an RPC just started.
type InitStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// Method is the full RPC method string, i.e., /package.service/method.
	Method string
	// RemoteAddr is the remote address of the corresponding connection.
	RemoteAddr net.Addr
	// LocalAddr is the local address of the corresponding connection.
	LocalAddr net.Addr
	// Encryption is encrypt method used in the RPC.
	Encryption string
}

func (s *InitStats) isStats() {}

// IncomingPayloadStats contains the information for a incoming payload.
type IncomingPayloadStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// Payload is the payload with original type.
	Payload interface{}
	// Data is the unencrypted message payload.
	Data []byte
	// Length is the length of uncompressed data.
	Length int
	// WireLength is the length of data on wire (compressed, signed, encrypted).
	WireLength int
	// ReceivedTime is the time when the payload is received.
	ReceivedTime time.Time
}

func (s *IncomingPayloadStats) isStats() {}

// IncomingHeaderStats indicates a header is received.
type IncomingHeaderStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// WireLength is the wire length of header.
	WireLength int
}

func (s *IncomingHeaderStats) isStats() {}

// IncomingTrailerStats indicates a trailer is received.
type IncomingTrailerStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// WireLength is the wire length of header.
	WireLength int
}

func (s *IncomingTrailerStats) isStats() {}

// OutgoingPayloadStats contains the information for a outgoing payload.
type OutgoingPayloadStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// Payload is the payload with original type.
	Payload interface{}
	// Data is the unencrypted message payload.
	Data []byte
	// Length is the length of uncompressed data.
	Length int
	// WireLength is the length of data on wire (compressed, signed, encrypted).
	WireLength int
	// SentTime is the time when the payload is sent.
	SentTime time.Time
}

func (s *OutgoingPayloadStats) isStats() {}

// OutgoingHeaderStats indicates a header is sent.
type OutgoingHeaderStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// WireLength is the wire length of header.
	WireLength int
}

func (s *OutgoingHeaderStats) isStats() {}

// OutgoingTrailerStats indicates a trailer is sent.
type OutgoingTrailerStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// WireLength is the wire length of header.
	WireLength int
}

func (s *OutgoingTrailerStats) isStats() {}

// ErrorStats indicates an error happens.
type ErrorStats struct {
	// IsClient indicates if this stats is a client stats.
	IsClient bool
	// Error is the error just happened. Its type is gRPC error.
	Error error
}

func (s *ErrorStats) isStats() {}

var (
	on      = new(int32)
	handler func(context.Context, Stats)
)

// On indicates whether stats is started.
func On() bool {
	return atomic.LoadInt32(on) == 1
}

// Handle returns the call back function registered by user to process the stats.
func Handle(ctx context.Context, s Stats) {
	handler(ctx, s)
}

// RegisterHandler registers the user handler function and starts the stats collection.
// This handler function will be called to process the stats.
func RegisterHandler(f func(context.Context, Stats)) {
	handler = f
	start()
}

// start starts the stats collection.
func start() {
	atomic.StoreInt32(on, 1)
}

// Stop stops the collection of any further stats.
func Stop() {
	atomic.StoreInt32(on, 0)
}
