// TODO(@gregorycooke) - Remove when only golang 1.19+ is supported
//go:build go1.19

/*
 *
 * Copyright 2021 gRPC authors.
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

package revocation

import (
	"google.golang.org/grpc/grpclog"
	internalrevocation "google.golang.org/grpc/security/advancedtls/internal/revocation"
)

var grpclogLogger = grpclog.Component("revocation")

// Cache is an interface to cache CRL files.
// The cache implementation must be concurrency safe.
// A fixed size lru cache from golang-lru is recommended.
type Cache = internalrevocation.Cache

// RevocationConfig contains options for CRL lookup.
type RevocationConfig = internalrevocation.RevocationConfig

// RevocationStatus is the revocation status for a certificate or chain.
type RevocationStatus = internalrevocation.RevocationStatus

// CRL contains a pkix.CertificateList and parsed extensions that aren't
// provided by the golang CRL parser.
// All CRLs should be loaded using NewCRL() for bytes directly or ReadCRLFile()
// to read directly from a filepath
type CRL = internalrevocation.CRL

// NewCRL constructs new CRL from the provided byte array.
func NewCRL(b []byte) (*CRL, error) {
	return internalrevocation.NewCRL(b)
}

// ReadCRLFile reads a file from the provided path, and returns constructed CRL
// struct from it.
func ReadCRLFile(path string) (*CRL, error) {
	return internalrevocation.ReadCRLFile(path)
}
