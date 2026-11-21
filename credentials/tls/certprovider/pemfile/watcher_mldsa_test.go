//go:build go1.27

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

package pemfile

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path"
	"testing"

	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/testdata"
)

func makeKeyMaterial(t *testing.T, certPath, keyPath, rootPath string) *certprovider.KeyMaterial {
	t.Helper()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(%q, %q) failed: %v", certPath, keyPath, err)
	}
	rootPEM, err := os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) failed: %v", rootPath, err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		t.Fatalf("AppendCertsFromPEM(%q) failed", rootPath)
	}
	return &certprovider.KeyMaterial{
		Certs: []tls.Certificate{cert},
		Roots: roots,
	}
}

// TestProvider_MLDSA tests that the file watcher provider loads and rotates
// ML-DSA-44, ML-DSA-65, ML-DSA-87, and ML-DSA-87 chain certificates and keys.
func (s) TestProvider_MLDSA(t *testing.T) {
	origDistributorFunc := newDistributor
	distCh := testutils.NewChannel()
	newDistributor = func() distributor { return newWrappedDistributor(distCh) }
	defer func() { newDistributor = origDistributorFunc }()

	dir, err := os.MkdirTemp("", "mldsa_provider_*")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	defer os.RemoveAll(dir)

	steps := []struct {
		certSrc string
		keySrc  string
		rootSrc string
	}{
		{
			certSrc: "x509/mldsa/server_mldsa44.pem",
			keySrc:  "x509/mldsa/server_mldsa44.key",
			rootSrc: "x509/mldsa/ca_mldsa44.pem",
		},
		{
			certSrc: "x509/mldsa/server_mldsa65.pem",
			keySrc:  "x509/mldsa/server_mldsa65.key",
			rootSrc: "x509/mldsa/ca_mldsa65.pem",
		},
		{
			certSrc: "x509/mldsa/server_mldsa87.pem",
			keySrc:  "x509/mldsa/server_mldsa87.key",
			rootSrc: "x509/mldsa/ca_mldsa87.pem",
		},
		{
			certSrc: "x509/mldsa/server_chain_mldsa87.pem",
			keySrc:  "x509/mldsa/server_chain_mldsa87.key",
			rootSrc: "x509/mldsa/ca_mldsa87.pem",
		},
	}

	createTmpFile(t, testdata.Path(steps[0].certSrc), path.Join(dir, certFile))
	createTmpFile(t, testdata.Path(steps[0].keySrc), path.Join(dir, keyFile))
	createTmpFile(t, testdata.Path(steps[0].rootSrc), path.Join(dir, rootFile))

	prov, err := NewProvider(Options{
		CertFile:        path.Join(dir, certFile),
		KeyFile:         path.Join(dir, keyFile),
		RootFile:        path.Join(dir, rootFile),
		RefreshDuration: defaultTestRefreshDuration,
	})
	if err != nil {
		t.Fatalf("NewProvider() failed: %v", err)
	}
	defer prov.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	for i, step := range steps {
		if i == 0 {
			for j := 0; j < 2; j++ {
				if _, err := distCh.Receive(ctx); err != nil {
					t.Fatalf("Step %d: timeout waiting for initial key material: %v", i, err)
				}
			}
		} else {
			createTmpFile(t, testdata.Path(step.certSrc), path.Join(dir, certFile))
			createTmpFile(t, testdata.Path(step.keySrc), path.Join(dir, keyFile))
			if _, err := distCh.Receive(ctx); err != nil {
				t.Fatalf("Step %d: timeout waiting for identity update: %v", i, err)
			}
			if step.rootSrc != steps[i-1].rootSrc {
				createTmpFile(t, testdata.Path(step.rootSrc), path.Join(dir, rootFile))
				if _, err := distCh.Receive(ctx); err != nil {
					t.Fatalf("Step %d: timeout waiting for root update: %v", i, err)
				}
			}
		}

		gotKM, err := prov.KeyMaterial(ctx)
		if err != nil {
			t.Fatalf("Step %d: prov.KeyMaterial() failed: %v", i, err)
		}
		wantKM := makeKeyMaterial(t, testdata.Path(step.certSrc), testdata.Path(step.keySrc), testdata.Path(step.rootSrc))
		if err := compareKeyMaterial(gotKM, wantKM); err != nil {
			t.Fatalf("Step %d: %v", i, err)
		}
	}
}
