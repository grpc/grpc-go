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

// The IdentityPEMFileProvider is tested in different stages.
// At stage 0, we create an IdentityPEMFileProvider with empty initial files.
// The KeyMaterial will be empty since the initial file contents are empty.
// At stage 1, we copy the first set of certFile and keyFile to the temp files
// that are watched by the goroutine.
// The KeyMaterial is expected to be updated if there is a matching key-cert pair.
// At stage 2, we copy the second set of certFile and keyFile to the temp files
// and verify the credential files are updated.
// The KeyMaterial is expected to be updated if there is a matching key-cert pair.
// At stage 3, we clear the file contents of temp files.
// The KeyMaterial is expected to skip the update because the file contents are empty.
func (s) TestIdentityPEMFileProvider(t *testing.T) {
	// Load certificates.
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Errorf("cs.loadCerts() failed: %v", err)
	}
	// Create temp files that are used to hold identity credentials.
	certTmp, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(certTmp.Name())
	keyTmp, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(keyTmp.Name())
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
		{
			desc:           "Update failed due to key-cert mismatch",
			certFileBefore: testdata.Path("server_cert_1.pem"),
			keyFileBefore:  testdata.Path("server_key_1.pem"),
			wantKmBefore:   certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer1}},
			certFileAfter:  testdata.Path("server_cert_1.pem"),
			keyFileAfter:   testdata.Path("server_key_2.pem"),
			wantKmAfter:    certprovider.KeyMaterial{Certs: []tls.Certificate{cs.serverPeer1}},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			identityPEMFileProviderOptions := &IdentityPEMFileProviderOptions{
				CertFile: certTmp.Name(),
				KeyFile:  keyTmp.Name(),
				Interval: 1 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			identityPEMFileProvider, err := NewIdentityPEMFileProvider(identityPEMFileProviderOptions)
			if err != nil {
				t.Errorf("NewIdentityPEMFileProvider(identityPEMFileProviderOptions) failed: %v", err)
			}
			gotKM, err := identityPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, certprovider.KeyMaterial{}, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, certprovider.KeyMaterial{})
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.certFileBefore, certTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.certFileBefore, certTmp): %v", err)
			}
			err = copyFileContents(test.keyFileBefore, keyTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.keyFileBefore, keyTmp): %v", err)
			}
			time.Sleep(2 * time.Second)
			gotKM, err = identityPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 2------------------------------------
			err = copyFileContents(test.certFileAfter, certTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.certFileAfter, certTmp): %v", err)
			}
			err = copyFileContents(test.keyFileAfter, keyTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.keyFileAfter, keyTmp): %v", err)
			}
			time.Sleep(2 * time.Second)
			gotKM, err = identityPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			// ------------------------Stage 3------------------------------------
			certTmp.Truncate(0)
			keyTmp.Truncate(0)
			time.Sleep(2 * time.Second)
			gotKM, err = identityPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			identityPEMFileProvider.Close()
		})
	}
}

// The RootPEMFileProvider is tested in different stages.
// At stage 0, we create an RootPEMFileProvider with empty initial file.
// The KeyMaterial will be empty since the initial file content is empty.
// At stage 1, we copy the first set of certFile and keyFile to the temp files
// that are watched by the goroutine. The KeyMaterial is expected to be updated.
// At stage 2, we copy the second set of certFile and keyFile to the temp files
// and verify the credential files are updated. The KeyMaterial is expected to be updated.
// At stage 3, we clear the file contents of temp files.
// The KeyMaterial is expected to skip the update because the file contents are empty.
func (s) TestRootPEMFileProvider(t *testing.T) {
	cs := &certStore{}
	err := cs.loadCerts()
	if err != nil {
		t.Errorf("cs.loadCerts() failed: %v", err)
	}
	// Create temp files that are used to hold root credentials.
	trustTmp, err := ioutil.TempFile(os.TempDir(), "pre-")
	if err != nil {
		t.Errorf("ioutil.TempFile(os.TempDir(), pre-) failed: %v", err)
	}
	defer os.Remove(trustTmp.Name())
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
			rootPEMFileProviderOptions := &RootPEMFileProviderOptions{
				TrustFile: trustTmp.Name(),
				Interval:  1 * time.Second,
			}
			// ------------------------Stage 0------------------------------------
			rootPEMFileProvider, err := NewRootPEMFileProvider(rootPEMFileProviderOptions)
			if err != nil {
				t.Errorf("NewRootPEMFileProvider(rootPEMFileProviderOptions) failed: %v", err)
			}
			gotKM, err := rootPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, certprovider.KeyMaterial{}, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, certprovider.KeyMaterial{})
			}
			// ------------------------Stage 1------------------------------------
			err = copyFileContents(test.trustFileBefore, trustTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.trustFileBefore, trustTmp): %v", err)
			}
			time.Sleep(2 * time.Second)
			gotKM, err = rootPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmBefore, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmBefore)
			}
			// ------------------------Stage 2------------------------------------
			err = copyFileContents(test.trustFileAfter, trustTmp.Name())
			if err != nil {
				t.Errorf("copyFileContents(test.trustFileAfter, trustTmp): %v", err)
			}
			time.Sleep(2 * time.Second)
			gotKM, err = rootPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			// ------------------------Stage 3------------------------------------
			trustTmp.Truncate(0)
			time.Sleep(2 * time.Second)
			gotKM, err = rootPEMFileProvider.KeyMaterial(context.Background())
			if !cmp.Equal(*gotKM, test.wantKmAfter, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmAfter)
			}
			rootPEMFileProvider.Close()
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
