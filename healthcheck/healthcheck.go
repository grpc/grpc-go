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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/status"
)

func init() {
	internal.HealthCheckFunc = newClientHealthCheck
}

func newClientHealthCheck(newStream func() (grpc.ClientStream, error), update func(bool, error), service string) {
	for {
		cs, err := newStream()
		if err != nil {
			//TODO: check ac current state? ctx cancelled?
			continue
		}
		req := &healthpb.HealthCheckRequest{
			Service: service,
		}
		if err := cs.SendMsg(req); err != nil {
			//TODO: check ac current state? ctx cancelled?
			if status.Code(err) == codes.Unimplemented {
				update(true, err)
				return
			}
			continue
		}
		if err := cs.CloseSend(); err != nil {
			//TODO: check ac current state? ctx cancelled?
			continue
		}
		for {
			resp := new(healthpb.HealthCheckResponse)
			err := cs.RecvMsg(resp)
			if err != nil {
				//transition to transient failure
				break
			}
			switch resp.GetStatus() {
			case healthpb.HealthCheckResponse_UNKNOWN:
				update(false, nil)
			case healthpb.HealthCheckResponse_SERVING:
				update(true, nil)
			case healthpb.HealthCheckResponse_NOT_SERVING:
				update(false, nil)
			case healthpb.HealthCheckResponse_SERVICE_UNKNOWN:
				update(false, nil)
			}
		}
	}
}
