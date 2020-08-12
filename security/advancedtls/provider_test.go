/*
 *
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

package advancedtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

// The IdentityPemFileProvider is tested in different stages.
// At stage 0, we copy the first set of certFile and keyFile to the temp files
// that are watched by the goroutine.
// At stage 1, we copy the second set of certFile and keyFile to the temp files
// and verify the credential files are updated.
func (s) TestIdentityPemFileProvider(t *testing.T) {
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Fatalf("cs.loadCerts() failed: %v", err)
	}
	tests := []struct {
		desc           string
		certFileBefore string
		keyFileBefore  string
		wantKmBefore   certprovider.KeyMaterial
		certFileAfter  string
		keyFileAfter   string
		wantKmAfter    certprovider.KeyMaterial
	}{
		{
			desc:           "Test identity provider on the client side",
			certFileBefore: testdata.Path("client_cert_1.pem"),
			keyFileBefore:  testdata.Path("client_key_1.pem"),
			wantKmBefore:   certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}},
			certFileAfter:  testdata.Path("client_cert_2.pem"),
			keyFileAfter:   testdata.Path("client_key_2.pem"),
			wantKmAfter:    certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer2}},
		},
		{
			desc:           "Test identity provider on the server side",
			certFileBefore: testdata.Path("server_cert_1.pem"),
			keyFileBefore:  testdata.Path("server_key_1.pem"),
			wantKmBefore:   certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer1}},
			certFileAfter:  testdata.Path("server_cert_2.pem"),
			keyFileAfter:   testdata.Path("server_key_2.pem"),
			wantKmAfter:    certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer2}},
		},
	}
	// Create temp files that are used to hold identity credentials.
	certTmpPath := testdata.Path("cert_tmp.pem")
	keyTmpPath := testdata.Path("key_tmp.pem")
	_, err = os.Create(certTmpPath)
	if err != nil {
		t.Fatalf("os.Create(certTmpPath) failed: %v", err)
	}
	_, err = os.Create(keyTmpPath)
	if err != nil {
		t.Fatalf("os.Create(keyTmpPath) failed: %v", err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			identityPemFileProviderOptions := &IdentityPemFileProviderOptions{
				CertFile: certTmpPath,
				KeyFile:  keyTmpPath,
				Interval: 3 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			err = copyFileContents(test.certFileBefore, certTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.certFileBefore, certTmpPath): %v", err)
			}
			err = copyFileContents(test.keyFileBefore, keyTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.keyFileBefore, keyTmpPath): %v", err)
			}
			quit := make(chan bool)
			identityPemFileProvider, err := NewIdentityPemFileProvider(identityPemFileProviderOptions, quit)
			if err != nil {
				t.Fatalf("NewIdentityPemFileProvider(identityPemFileProviderOptions) failed: %v", err)
			}
			gotKM, err := identityPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.certFileAfter, certTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.certFileAfter, certTmpPath): %v", err)
			}
			err = copyFileContents(test.keyFileAfter, keyTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.keyFileAfter, keyTmpPath): %v", err)
			}
			time.Sleep(5 * time.Second)
			gotKM, err = identityPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			quit <- true
		})
	}
	// Remove temp files.
	if _, err := os.Stat(certTmpPath); err == nil {
		err := os.Remove(certTmpPath)
		if err != nil {
			t.Fatalf("os.Remove(certTmpPath) failed: %v", err)
		}
	}
	if _, err := os.Stat(keyTmpPath); err == nil {
		err := os.Remove(keyTmpPath)
		if err != nil {
			t.Fatalf("os.Remove(keyTmpPath) failed: %v", err)
		}
	}
}

// The RootPemFileProvider is tested in different stages.
// At stage 0, we copy the first trustFile to the temp file that are watched by the goroutine.
// At stage 1, we copy the second trustFile to the temp file and verify the credential files are updated.
func (s) TestRootPemFileProvider(t *testing.T) {
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Fatalf("cs.loadCerts() failed: %v", err)
	}
	tests := []struct {
		desc            string
		trustFileBefore string
		wantKmBefore    certprovider.KeyMaterial
		trustFileAfter  string
		wantKmAfter     certprovider.KeyMaterial
	}{
		{
			desc:            "Test root provider on the client side",
			trustFileBefore: testdata.Path("client_trust_cert_1.pem"),
			wantKmBefore:    certprovider.KeyMaterial{Roots: cs.clientTrust1},
			trustFileAfter:  testdata.Path("client_trust_cert_2.pem"),
			wantKmAfter:     certprovider.KeyMaterial{Roots: cs.clientTrust2},
		},
		{
			desc:            "Test root provider on the server side",
			trustFileBefore: testdata.Path("server_trust_cert_1.pem"),
			wantKmBefore:    certprovider.KeyMaterial{Roots: cs.serverTrust1},
			trustFileAfter:  testdata.Path("server_trust_cert_2.pem"),
			wantKmAfter:     certprovider.KeyMaterial{Roots: cs.serverTrust2},
		},
	}
	// Create temp files that are used to hold identity credentials.
	trustTmpPath := testdata.Path("trust_tmp.pem")
	_, err = os.Create(trustTmpPath)
	if err != nil {
		t.Fatalf("os.Create(trustTmpPath) failed: %v", err)
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			rootPemFileProviderOptions := &RootPemFileProviderOptions{
				TrustFile: trustTmpPath,
				Interval:  3 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			err = copyFileContents(test.trustFileBefore, trustTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.trustFileBefore, trustTmpPath): %v", err)
			}
			quit := make(chan bool)
			rootPemFileProvider, err := NewRootPemFileProvider(rootPemFileProviderOptions, quit)
			if err != nil {
				t.Fatalf("NewRootPemFileProvider(rootPemFileProviderOptions) failed: %v", err)
			}
			gotKM, err := rootPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.trustFileAfter, trustTmpPath)
			if err != nil {
				t.Fatalf("copyFileContents(test.trustFileAfter, trustTmpPath): %v", err)
			}
			time.Sleep(5 * time.Second)
			gotKM, err = rootPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			quit <- true
		})
	}
	// Remove temp files.
	if _, err := os.Stat(trustTmpPath); err == nil {
		err := os.Remove(trustTmpPath)
		if err != nil {
			t.Fatalf("os.Remove(trustTmpPath) failed: %v", err)
		}
	}
}

func copyFileContents(sourceFile, destinationFile string) error {
	input, err := ioutil.ReadFile(sourceFile)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(destinationFile, input, 0644)
	if err != nil {
		return err
	}
	return nil
}
