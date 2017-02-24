/*
 *
 * Copyright 2017, Google Inc.
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

// Package proxy defines interfaces to support proxyies in gRPC.
package proxy // import "google.golang.org/grpc/proxy"
import (
	"errors"

	"golang.org/x/net/context"
)

// ErrIneffective indicates the mapper function is not effective.
var ErrIneffective = errors.New("Mapper function is not effective")

// Mapper defines the interface gRPC uses to map the proxy address.
type Mapper interface {
	// MapName is called before the server name is resolved.
	// It can be used to programmatically override the name that will be resolved.
	// It returns the URI of the proxy, and the header to be sent in the request.
	// ErrIneffective should be returned if the function is not effective.
	MapName(ctx context.Context, uri string) (string, map[string][]string, error)
	// MapAddress is called before we connect to the target address.
	// It can be used to programmatically override the address that we will connect to.
	// It returns the address of the proxy, and the header to be sent in the request.
	// ErrIneffective should be returned if the function is not effective.
	MapAddress(ctx context.Context, uri string, address string) (string, map[string][]string, error)
}
