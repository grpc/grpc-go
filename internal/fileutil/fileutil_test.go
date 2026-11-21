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

package fileutil_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/internal/fileutil"
)

func writeTempFile(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, contents, 0600); err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
	}
	return path
}

func TestReadFile(t *testing.T) {
	tests := []struct {
		desc     string
		contents []byte
		wantErr  string
	}{
		{
			desc:     "empty file",
			contents: []byte{},
		},
		{
			desc:     "small file",
			contents: []byte("hello"),
		},
		{
			desc:     "exactly at the limit",
			contents: make([]byte, fileutil.MaxConfigFileSize),
		},
		{
			desc:     "one byte over the limit",
			contents: make([]byte, fileutil.MaxConfigFileSize+1),
			wantErr:  "exceeds maximum size",
		},
		{
			desc:     "well over the limit",
			contents: make([]byte, fileutil.MaxConfigFileSize+1024),
			wantErr:  "exceeds maximum size",
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			path := writeTempFile(t, test.contents)
			got, err := fileutil.ReadFile(path)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ReadFile(%q) failed: %v", path, err)
				}
				if len(got) != len(test.contents) {
					t.Fatalf("ReadFile(%q) returned %d bytes, want %d", path, len(got), len(test.contents))
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("ReadFile(%q) returned err: %v, want err containing %q", path, err, test.wantErr)
			}
		})
	}
}

func TestReadFileMissing(t *testing.T) {
	if _, err := fileutil.ReadFile(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("ReadFile() on a missing file returned no error")
	}
}
