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

package healthcheck

import (
	"errors"
	"io"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/status"
)

func init() {
	internal.HealthCheckFunc = clientHealthCheck
}

const maxDelay = 5 * time.Second

var backoffFunc = func(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		timer.Stop()
		return false
	}
}

func clientHealthCheck(ctx context.Context, newStream func() (interface{}, error), reportHealth func(bool), service string) error {
	tryCnt := 0
	bo := backoff.Exponential{MaxDelay: maxDelay}

retryConnection:
	for {
		// Backs off if the connection has failed in some way without receiving a message in the previous retry.
		if tryCnt > 0 && !backoffFunc(ctx, bo.Backoff(tryCnt-1)) {
			return nil
		}
		tryCnt++

		if ctx.Err() != nil {
			return nil
		}
		rawS, err := newStream()
		if err != nil {
			continue retryConnection
		}

		s, ok := rawS.(grpc.ClientStream)
		if !ok {
			// exit the health check function
			return errors.New("type assertion to grpc.ClientStream failed")
		}

		if err = s.SendMsg(&healthpb.HealthCheckRequest{Service: service}); err != nil && err != io.EOF {
			//stream should have been closed, so we can safely continue to create a new stream.
			continue retryConnection
		}
		s.CloseSend()

		resp := new(healthpb.HealthCheckResponse)
		for {
			err = s.RecvMsg(resp)

			// Reports healthy for the LBing purposes if health check is not implemented in the server.
			if status.Code(err) == codes.Unimplemented {
				reportHealth(true)
				return err
			}

			// Reports unhealthy if server's Watch method gives an error other than UNIMPLEMENTED.
			if err != nil {
				reportHealth(false)
				continue retryConnection
			}

			// As a message has been received, removes the need for backoff for the next retry by reseting the try count.
			tryCnt = 0
			reportHealth(resp.Status == healthpb.HealthCheckResponse_SERVING)
		}
	}
}
