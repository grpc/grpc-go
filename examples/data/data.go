/*
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

// Package data provides convenience routines to access files in the data
// directory.
package data

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"runtime"
)

// basepath is the root directory of this package.
var basepath string

func init() {
	_, currentFile, _, _ := runtime.Caller(0)
	basepath = filepath.Dir(currentFile)
}

// Path returns the absolute path the given relative file or directory path,
// relative to the google.golang.org/grpc/examples/data directory in the
// user's GOPATH.  If rel is already absolute, it is returned unmodified.
func Path(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}

	return filepath.Join(basepath, rel)
}

// NewCertPool returns a x509.CertPool given the absolute paths of a list of
// PEM certificates, or an error if any failure is encountered when loading
// the said certificates.
func NewCertPool(certPaths ...string) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	for _, p := range certPaths {
		caBytes, err := ioutil.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("failed to read cert %q: %v", p, err)
		}
		if ok := certPool.AppendCertsFromPEM(caBytes); !ok {
			return nil, fmt.Errorf("failed to parse %q", p)
		}
	}
	return certPool, nil
}
