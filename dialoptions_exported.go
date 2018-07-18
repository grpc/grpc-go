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

// This file contains DialOptions that are exported so users can intercept them.

package grpc

import (
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/stats"
)

// WithTransportCredentials returns a DialOption which configures a connection
// level security credentials (e.g., TLS/SSL).
func WithTransportCredentials(creds credentials.TransportCredentials) DialOption {
	return TransportCredentialsDialOption{
		creds: creds,
	}
}

// WithInsecure returns a DialOption which disables transport security for this
// ClientConn. Note that transport security is required unless WithInsecure is
// set.
func WithInsecure() DialOption {
	return TransportCredentialsDialOption{
		creds: nil,
	}
}

// TransportCredentialsDialOption is a DialOption that configures a connection
// level security credentials (e.g., TLS/SSL).
type TransportCredentialsDialOption struct {
	// If creds is nil, insecure will be set for the ClientConn.
	creds credentials.TransportCredentials
}

func (tcdo TransportCredentialsDialOption) apply(do *dialOptions) {
	if tcdo.creds != nil {
		do.copts.TransportCredentials = tcdo.creds
	} else {
		do.insecure = true
	}
}

// WithStatsHandler returns a DialOption that specifies the stats handler for
// all the RPCs and underlying network connections in this ClientConn.
func WithStatsHandler(h stats.Handler) DialOption {
	return StatsHandlerDialOption{
		handler: h,
	}
}

// StatsHandlerDialOption is a DialOption that specifies the stats handler for
// all the RPCs and underlying network connections.
type StatsHandlerDialOption struct {
	handler stats.Handler
}

func (shdo StatsHandlerDialOption) apply(do *dialOptions) {
	do.copts.StatsHandler = shdo.handler
}

func withContextDialer(f func(context.Context, string) (net.Conn, error)) DialOption {
	return ContextDialerDialOption{
		dial: f,
	}
}

// ContextDialerDialOption is a DialOption that specifies a function to use for
// dialing network addresses.
type ContextDialerDialOption struct {
	dial func(context.Context, string) (net.Conn, error)
}

func (cddo ContextDialerDialOption) apply(do *dialOptions) {
	do.copts.Dialer = cddo.dial
}

// WithKeepaliveParams returns a DialOption that specifies keepalive parameters
// for the client transport.
func WithKeepaliveParams(kp keepalive.ClientParameters) DialOption {
	return KeepaliveParamsDialOption{
		kp: kp,
	}
}

// KeepaliveParamsDialOption is a DialOption that specifies keepalive
// parameters.
type KeepaliveParamsDialOption struct {
	kp keepalive.ClientParameters
}

func (kpdo KeepaliveParamsDialOption) apply(do *dialOptions) {
	do.copts.KeepaliveParams = kpdo.kp
}

// WithUnaryInterceptor returns a DialOption that specifies the interceptor for
// unary RPCs.
func WithUnaryInterceptor(f UnaryClientInterceptor) DialOption {
	return UnaryInterceptorDialOption{
		Interceptor: f,
	}
}

// UnaryInterceptorDialOption is a DialOption that specifies the interceptor for
// unary RPCs.
type UnaryInterceptorDialOption struct {
	// Interceptor is the interceptor to be installed.
	Interceptor UnaryClientInterceptor
}

func (uido UnaryInterceptorDialOption) apply(do *dialOptions) {
	do.unaryInt = uido.Interceptor
}

// WithStreamInterceptor returns a DialOption that specifies the interceptor for
// streaming RPCs.
func WithStreamInterceptor(f StreamClientInterceptor) DialOption {
	return StreamInterceptorDialOption{
		Interceptor: f,
	}
}

// StreamInterceptorDialOption is a DialOption that specifies the interceptor
// for streaming RPCs.
type StreamInterceptorDialOption struct {
	// Interceptor is the interceptor to be installed.
	Interceptor StreamClientInterceptor
}

func (sido StreamInterceptorDialOption) apply(do *dialOptions) {
	do.streamInt = sido.Interceptor
}
