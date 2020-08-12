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
	// Load certificates.
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Errorf("cs.loadCerts() failed: %v", err)
	}
	// Create temp files that are used to hold identity credentials.
	certTmpPath, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(certTmpPath.Name())
	keyTmpPath, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(keyTmpPath.Name())
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
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			identityPemFileProviderOptions := &IdentityPemFileProviderOptions{
				CertFile: certTmpPath.Name(),
				KeyFile:  keyTmpPath.Name(),
				Interval: 3 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			err = copyFileContents(test.certFileBefore, certTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.certFileBefore, certTmpPath): %v", err)
			}
			err = copyFileContents(test.keyFileBefore, keyTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.keyFileBefore, keyTmpPath): %v", err)
			}
			identityPemFileProvider, err := NewIdentityPemFileProvider(identityPemFileProviderOptions)
			if err != nil {
				t.Errorf("NewIdentityPemFileProvider(identityPemFileProviderOptions) failed: %v", err)
			}
			gotKM, err := identityPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.certFileAfter, certTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.certFileAfter, certTmpPath): %v", err)
			}
			err = copyFileContents(test.keyFileAfter, keyTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.keyFileAfter, keyTmpPath): %v", err)
			}
			time.Sleep(5 * time.Second)
			gotKM, err = identityPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			identityPemFileProvider.Close()
		})
	}
}

// The RootPemFileProvider is tested in different stages.
// At stage 0, we copy the first trustFile to the temp file that are watched by the goroutine.
// At stage 1, we copy the second trustFile to the temp file and verify the credential files are updated.
func (s) TestRootPemFileProvider(t *testing.T) {
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Errorf("cs.loadCerts() failed: %v", err)
	}
	// Create temp files that are used to hold root credentials.
	trustTmpPath, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(trustTmpPath.Name())
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
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			rootPemFileProviderOptions := &RootPemFileProviderOptions{
				TrustFile: trustTmpPath.Name(),
				Interval:  3 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			err = copyFileContents(test.trustFileBefore, trustTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.trustFileBefore, trustTmpPath): %v", err)
			}
			rootPemFileProvider, err := NewRootPemFileProvider(rootPemFileProviderOptions)
			if err != nil {
				t.Errorf("NewRootPemFileProvider(rootPemFileProviderOptions) failed: %v", err)
			}
			gotKM, err := rootPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.trustFileAfter, trustTmpPath.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.trustFileAfter, trustTmpPath): %v", err)
			}
			time.Sleep(5 * time.Second)
			gotKM, err = rootPemFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			rootPemFileProvider.Close()
		})
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
