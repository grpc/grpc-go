/*
 *
 * Copyright 2023 gRPC authors.
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

package encoding

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/internal/grpcutil"
)

type mockNamedCompressor struct {
	Compressor
}

func (mockNamedCompressor) Name() string {
	return "mock-compressor"
}

func TestDuplicateCompressorRegister(t *testing.T) {
	defer func(m map[string]Compressor) { registeredCompressor = m }(registeredCompressor)
	defer func(c []string) { grpcutil.RegisteredCompressorNames = c }(grpcutil.RegisteredCompressorNames)
	registeredCompressor = map[string]Compressor{}
	grpcutil.RegisteredCompressorNames = []string{}

	RegisterCompressor(&mockNamedCompressor{})

	// Register another instance of the same compressor.
	mc := &mockNamedCompressor{}
	RegisterCompressor(mc)
	if got := registeredCompressor["mock-compressor"]; got != mc {
		t.Fatalf("Unexpected compressor, got: %+v, want:%+v", got, mc)
	}

	wantNames := []string{"mock-compressor"}
	if !cmp.Equal(wantNames, grpcutil.RegisteredCompressorNames) {
		t.Fatalf("Unexpected compressor names, got: %+v, want:%+v", grpcutil.RegisteredCompressorNames, wantNames)
	}
}
