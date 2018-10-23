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
	internal.HealthCheckFunc = newClientHealthCheck
}

const maxDelay time.Duration = 5 * time.Second

func newClientHealthCheck(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
	retryCnt := 0
	needsBackoff := false
	bo := backoff.Exponential{MaxDelay: maxDelay}

retryConnection:
	for {
		// Backs off if the connection has failed in some way without receiving a message in the previous retry.
		if needsBackoff {
			timer := time.NewTimer(bo.Backoff(retryCnt))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil
			}
			retryCnt++
		}

		// Assumes that the connection will fail without receiving a message and we will need a backoff before the next retry.
		// If everything goes alright and we receive a message before connection fails, then this is set to false below.
		needsBackoff = true

		select {
		case <-ctx.Done():
			return nil
		default:
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

		req := healthpb.HealthCheckRequest{
			Service: service,
		}
		if err = s.SendMsg(&req); err != nil && err != io.EOF {
			//stream should have been closed, so we can safely continue to create a new stream.
			continue retryConnection
		}
		s.CloseSend()

		for {
			resp := new(healthpb.HealthCheckResponse)
			err = s.RecvMsg(resp)

			// Reports healthy for the LBing purposes if health check is not implemented in the server.
			if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
				update(true)
				return err
			}

			// Reports unhealthy if server's Watch method gives an error other than UNIMPLEMENTED.
			if err != nil {
				update(false)
				continue retryConnection
			}

			// As a message has been received, removes the need for backoff for the next retry and resets the retry count.
			retryCnt = 0
			needsBackoff = false
			if resp.Status == healthpb.HealthCheckResponse_SERVING {
				update(true)
			} else {
				update(false)
			}
		}
	}
}
