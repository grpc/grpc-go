/*
 *
 * Copyright 2024 gRPC authors.
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

package transport

import (
	"context"
	"errors"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/status"
)

var ErrGrpcTimeout = errors.New("grpc-timeout")
var ErrRequestDone = errors.New("request is done processing")
var ErrServerTransportClosed = errors.New("server transport closed")
var ErrUnreachable = errors.New("unreachable")

type RstCodeError struct {
	RstCode http2.ErrCode
}

func (e RstCodeError) Error() string {
	return e.RstCode.String()
}

type StatusError struct {
	Status *status.Status
}

func (e StatusError) Error() string {
	return e.Status.String()
}

func createContext(ctx context.Context, timeoutSet bool, timeout time.Duration) (context.Context, context.CancelCauseFunc) {
	var timoutCancel context.CancelFunc = nil
	if timeoutSet {
		ctx, timoutCancel = context.WithTimeoutCause(ctx, timeout, ErrGrpcTimeout)
	}
	ctx, cancel := context.WithCancelCause(ctx)
	if timoutCancel != nil {
		context.AfterFunc(ctx, timoutCancel)
	}
	return ctx, cancel
}
