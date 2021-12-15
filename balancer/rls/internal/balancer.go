/*
 *
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

package rls

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/grpclog"
)

var (
	_ balancer.Balancer = (*rlsBalancer)(nil)

	logger = grpclog.Component("rls")
)

// rlsBalancer implements the RLS LB policy.
type rlsBalancer struct{}

func (lb *rlsBalancer) UpdateClientConnState(ccs balancer.ClientConnState) error {
	logger.Fatal("rls: UpdateClientConnState is not yet unimplemented")
	return nil
}

func (lb *rlsBalancer) ResolverError(error) {
	logger.Fatal("rls: ResolverError is not yet unimplemented")
}

func (lb *rlsBalancer) UpdateSubConnState(_ balancer.SubConn, _ balancer.SubConnState) {
	logger.Fatal("rls: UpdateSubConnState is not yet implemented")
}

func (lb *rlsBalancer) Close() {
	logger.Fatal("rls: Close is not yet implemented")
}

func (lb *rlsBalancer) ExitIdle() {
	logger.Fatal("rls: ExitIdle is not yet implemented")
}
