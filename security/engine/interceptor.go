/*
 * Copyright 2020 gRPC authors.
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

package engine

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

var (
	errMissingPeerInformation 	= status.Errorf(codes.InvalidArgument, "missing peer information")
	errUnauthorized 			= status.Errorf(codes.PermissionDenied, "unauthorized")
)

// Returns whether or not a given context is authorized
func authorized(ctx context.Context) (bool, error) {
	peerInfo, ok := peer.FromContext(ctx)
	if !ok {
		return false, errMissingPeerInformation
	}
	return evaluate(peerInfo), nil
}

// The unary interceptor that incorporates the CEL engine into the server
func unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// authorization (CEL engine)
	auth, err := authorized(ctx)
	if err != nil {
		return nil, err
	} else if !auth {
		return nil, errUnauthorized
	}
	// invoking handler
	m, err := handler(ctx, req)
	return m, err
}

// wrappedStream wraps around the embedded grpc.ServerStream, and intercepts the RecvMsg and
// SendMsg method call.
type wrappedStream struct {
	grpc.ServerStream
}

func (w *wrappedStream) RecvMsg(m interface{}) error {
	return w.ServerStream.RecvMsg(m)
}

func (w *wrappedStream) SendMsg(m interface{}) error {
	return w.ServerStream.SendMsg(m)
}

func newWrappedStream(s grpc.ServerStream) grpc.ServerStream {
	return &wrappedStream{s}
}

// The stream interceptor that incorporates the CEL engine into the server
func streamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// authorization (CEL engine)
	auth, err := authorized(ss.Context())
	if err != nil {
		return err
	} else if !auth {
		return errUnauthorized
	}
	// invoking handler
	err = handler(srv, newWrappedStream(ss))
	return err
}
