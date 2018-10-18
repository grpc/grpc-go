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

package healthcheck

import (
	"context"
	"errors"
	"io"
	"time"

	"google.golang.org/grpc/internal/backoff"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/status"
)

func init() {
	internal.HealthCheckFunc = newClientHealthCheck
}

func newClientHealthCheck(ctx context.Context, newStream func() (interface{}, error), update func(bool), service string) error {
	retryCnt := 0
	doBackoff := false
	bo := backoff.Exponential{MaxDelay: 5 * time.Second}
retryConnection:
	for {
		if !doBackoff {
			retryCnt = 0
		} else {
			timer := time.NewTimer(bo.Backoff(retryCnt))
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil
			}
			retryCnt++
		}
		doBackoff = false

		select {
		case <-ctx.Done():
			return nil
		default:
		}
		rawS, err := newStream()
		if err != nil {
			doBackoff = true
			continue retryConnection
		}
		s, ok := rawS.(grpc.ClientStream)
		if !ok {
			// exit the health check function
			return errors.New("type assertion to grpc.ClientStream failed")
		}
		if err = s.SendMsg(&healthpb.HealthCheckRequest{
			Service: service,
		}); err != nil && err != io.EOF {
			//stream should have been closed, so we can safely continue to create a new stream.
			continue retryConnection
		}
		s.CloseSend()
		for {
			resp := new(healthpb.HealthCheckResponse)
			if err = s.RecvMsg(resp); err != nil {
				if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
					update(true)
					return err
				}
				// transition to TRANSIENT FAILURE when Watch() fails with status other than UNIMPLEMENTED
				update(false)
				// we can safely break here and continue to create a new stream, since a non-nil error has been received.
				continue retryConnection
			}
			switch resp.Status {
			case healthpb.HealthCheckResponse_SERVING:
				update(true)
			case healthpb.HealthCheckResponse_SERVICE_UNKNOWN, healthpb.HealthCheckResponse_UNKNOWN, healthpb.HealthCheckResponse_NOT_SERVING:
				update(false)
			}
		}
	}
}
