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

package main

import (
	"google.golang.org/grpc/examples/deadline/client"
	pb "google.golang.org/grpc/examples/deadline/deadline"
)

func main() {
	// A successful request
	client.ConnectAndRequest(&pb.DeadlinerRequest{})
	// Exceeds deadline
	client.ConnectAndRequest(&pb.DeadlinerRequest{Message: "delay"})
	// A successful request with propagated deadline
	client.ConnectAndRequest(&pb.DeadlinerRequest{Hops: 3})
	// Exceeds propagated deadline
	client.ConnectAndRequest(&pb.DeadlinerRequest{Hops: 4})
}
