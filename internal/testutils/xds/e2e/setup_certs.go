/*
 *
 * Copyright 2022 gRPC authors.
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

package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path"
	"testing"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/testdata"
)

const (
	// Names of files inside tempdir, for certprovider plugin to watch.
	certFile = "cert.pem"
	keyFile  = "key.pem"
	rootFile = "ca.pem"
)

func createTmpFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("os.ReadFile(%q) failed: %v", src, err)
	}
	if err := os.WriteFile(dst, data, os.ModePerm); err != nil {
		return fmt.Errorf("os.WriteFile(%q) failed: %v", dst, err)
	}
	return nil
}

// createTempDirWithFiles creates a temporary directory under the system default
// tempDir with the given dirSuffix. It also reads from certSrc, keySrc and
// rootSrc files are creates appropriate files under the newly create tempDir.
// Returns the name of the created tempDir.
func createTmpDirWithFiles(dirSuffix, certSrc, keySrc, rootSrc string) (string, error) {
	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := os.MkdirTemp("", dirSuffix)
	if err != nil {
		return "", fmt.Errorf("os.MkdirTemp() failed: %v", err)
	}

	if err := createTmpFile(testdata.Path(certSrc), path.Join(dir, certFile)); err != nil {
		return "", err
	}
	if err := createTmpFile(testdata.Path(keySrc), path.Join(dir, keyFile)); err != nil {
		return "", err
	}
	if err := createTmpFile(testdata.Path(rootSrc), path.Join(dir, rootFile)); err != nil {
		return "", err
	}
	return dir, nil
}

// CreateClientTLSCredentials creates client-side TLS transport credentials
// using certificate and key files from testdata/x509 directory.
func CreateClientTLSCredentials(t *testing.T) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/client1_cert.pem"), testdata.Path("x509/client1_key.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(x509/client1_cert.pem, x509/client1_key.pem) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("x509/server_ca_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(x509/server_ca_cert.pem) failed: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      roots,
		ServerName:   "x.test.example.com",
	})
}

// CreateServerTLSCredentials creates server-side TLS transport credentials
// using certificate and key files from testdata/x509 directory.
func CreateServerTLSCredentials(t *testing.T, clientAuth tls.ClientAuthType) credentials.TransportCredentials {
	t.Helper()

	cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(x509/server1_cert.pem, x509/server1_key.pem) failed: %v", err)
	}
	b, err := os.ReadFile(testdata.Path("x509/client_ca_cert.pem"))
	if err != nil {
		t.Fatalf("os.ReadFile(x509/client_ca_cert.pem) failed: %v", err)
	}
	ca := x509.NewCertPool()
	if !ca.AppendCertsFromPEM(b) {
		t.Fatal("Failed to append certificates")
	}
	return credentials.NewTLS(&tls.Config{
		ClientAuth:   clientAuth,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    ca,
	})
}
