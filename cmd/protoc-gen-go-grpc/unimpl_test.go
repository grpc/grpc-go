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

package main_test

import (
	"testing"

	"google.golang.org/grpc"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

type unimplEmbeddedByPointer struct {
	*testgrpc.UnimplementedTestServiceServer
}

type unimplEmbeddedByValue struct {
	testgrpc.UnimplementedTestServiceServer
}

func TestUnimplementedEmbedding(t *testing.T) {
	t.Skip("Skip until next grpc release includes panic during registration.")

	// Embedded by value, this should succeed.
	testgrpc.RegisterTestServiceServer(grpc.NewServer(), &unimplEmbeddedByValue{})
	defer func() {
		if recover() == nil {
			t.Fatalf("Expected panic; received none")
		}
	}()

	// Embedded by pointer, this should panic.
	testgrpc.RegisterTestServiceServer(grpc.NewServer(), &unimplEmbeddedByPointer{})
}
