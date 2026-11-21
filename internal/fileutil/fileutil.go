/*
 *
 * Copyright 2026 gRPC authors.
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

// Package fileutil contains helpers for reading local configuration files.
package fileutil

import (
	"fmt"
	"io"
	"os"
)

// MaxConfigFileSize is the maximum size, in bytes, of a local configuration
// file (for example, a TLS certificate, authorization policy, or
// observability config) read by gRPC. Files larger than this are rejected
// instead of being buffered in full.
const MaxConfigFileSize = 8 << 20 // 8 MiB.

// ReadFile reads the file named by path, returning an error if the file is
// larger than MaxConfigFileSize. It reads at most MaxConfigFileSize+1 bytes,
// so an over-limit file is detected without buffering it in full.
func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	contents, err := io.ReadAll(io.LimitReader(f, MaxConfigFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > MaxConfigFileSize {
		return nil, fmt.Errorf("file %q exceeds maximum size of %d bytes", path, MaxConfigFileSize)
	}
	return contents, nil
}
