/*
 *
 * Copyright 2019 gRPC authors.
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

// Package service is an empty package that exists so as to allow the `make
// proto` command to generate the appropriate .pb.go files for the profiling
// service from the .proto files. The `make proto` command, when called from
// gRPC's root directory, uses `go generate` to do this, which requires a
// go:generate comment. This is more standardized than expecting a dev to run
// protoc manually, which requires them to generate it in a specific way to
// keep references to the .proto unchanged.
package service

//go:generate protoc --go_out=plugins=grpc,paths=source_relative:. service.proto
