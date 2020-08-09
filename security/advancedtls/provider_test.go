/*
 *
 * Copyright 2019 gRPC authors.
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

func (s) TestIdentityPemFileProvider(t *testing.T) {
	clientCert, err := tls.LoadX509KeyPair(testdata.Path("client_cert_1.pem"), testdata.Path("client_key_1.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(client_cert_1.pem, client_key_1.pem) failed: %v", err)
	}
	clientKM := certprovider.KeyMaterial{Certs: []tls.Certificate{clientCert}}
	serverCert, err := tls.LoadX509KeyPair(testdata.Path("server_cert_1.pem"), testdata.Path("server_key_1.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(server_cert_1.pem, server_key_1.pem) failed: %v", err)
	}
	serverKM := certprovider.KeyMaterial{Certs: []tls.Certificate{serverCert}}
	tests := []struct {
		desc     string
		certFile string
		keyFile  string
		wantKM   certprovider.KeyMaterial
	}{
		{
			desc:     "Load client_cert_1.pem",
			certFile: testdata.Path("client_cert_1.pem"),
			keyFile:  testdata.Path("client_key_1.pem"),
			wantKM:   clientKM,
		},
		{
			desc:     "Load server_cert_1.pem",
			certFile: testdata.Path("server_cert_1.pem"),
			keyFile:  testdata.Path("server_key_1.pem"),
			wantKM:   serverKM,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			identityPemFileProviderOptions := &IdentityPemFileProviderOptions{
				CertFile: test.certFile,
				KeyFile:  test.keyFile,
				Interval: 10 * time.Second,
			}
			identityPemFileProvider, err := NewIdentityPemFileProvider(identityPemFileProviderOptions)
			if err != nil {
				t.Fatalf("NewIdentityPemFileProvider(identityPemFileProviderOptions) failed: %v", err)
			}
			gotDistributor := identityPemFileProvider.distributor
			ctx := context.Background()
			gotKM, err := gotDistributor.KeyMaterial(ctx)
			if !cmp.Equal(*gotKM, test.wantKM, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKM)
			}
		})
	}
}

func (s) TestRootPemFileProvider(t *testing.T) {
	clientTrustPool, err := readTrustCert(testdata.Path("client_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("readTrustCert(client_trust_cert_1.pem) failed: %v", err)
	}
	clientKM := certprovider.KeyMaterial{Roots: clientTrustPool}
	serverTrustPool, err := readTrustCert(testdata.Path("server_trust_cert_1.pem"))
	if err != nil {
		t.Fatalf("readTrustCert(server_trust_cert_1.pem) failed: %v", err)
	}
	serverKM := certprovider.KeyMaterial{Roots: serverTrustPool}
	tests := []struct {
		desc      string
		trustCert string
		wantKM    certprovider.KeyMaterial
	}{
		{
			desc:      "Load client_trust_cert_1.pem",
			trustCert: testdata.Path("client_trust_cert_1.pem"),
			wantKM:    clientKM,
		},
		{
			desc:      "Load server_trust_cert_1.pem",
			trustCert: testdata.Path("server_trust_cert_1.pem"),
			wantKM:    serverKM,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			rootPemFileProviderOptions := &RootPemFileProviderOptions{
				TrustFile: test.trustCert,
				Interval:  10 * time.Second,
			}
			rootPemFileProvider, err := NewRootPemFileProvider(rootPemFileProviderOptions)
			if err != nil {
				t.Fatalf("NewRootPemFileProvider(rootPemFileProviderOptions) failed: %v", err)
			}
			gotDistributor := rootPemFileProvider.distributor
			ctx := context.Background()
			gotKM, err := gotDistributor.KeyMaterial(ctx)
			if !cmp.Equal(*gotKM, test.wantKM, cmp.AllowUnexported(x509.CertPool{})) {
				t.Errorf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKM)
			}
		})
	}
}

func (s) TestCopyFileContents(t *testing.T) {
	clientCert1, err := tls.LoadX509KeyPair(testdata.Path("client_cert_1.pem"), testdata.Path("client_key_1.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(client_cert_1.pem, client_key_1.pem) failed: %v", err)
	}
	clientCert2, err := tls.LoadX509KeyPair(testdata.Path("client_cert_2.pem"), testdata.Path("client_key_2.pem"))
	if err != nil {
		t.Fatalf("tls.LoadX509KeyPair(client_cert_2.pem, client_key_2.pem) failed: %v", err)
	}
	tests := []struct {
		desc           string
		certFileBefore string
		keyFileBefore  string
		wantCertBefore tls.Certificate
		certFileAfter  string
		keyFileAfter   string
		wantCertAfter  tls.Certificate
	}{
		{
			desc:           "Test file copy",
			certFileBefore: testdata.Path("client_cert_1.pem"),
			keyFileBefore:  testdata.Path("client_key_1.pem"),
			wantCertBefore: clientCert1,
			certFileAfter:  testdata.Path("client_cert_2.pem"),
			keyFileAfter:   testdata.Path("client_key_2.pem"),
			wantCertAfter:  clientCert2,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			err := copyFileContents(test.certFileBefore, testdata.Path("client_cert_tmp.pem"))
			if err != nil {
				t.Fatalf("copyFileContents(test.certFileBefore, testdata.Path(client_cert_tmp.pem)): %v", err)
			}
			err = copyFileContents(test.keyFileBefore, testdata.Path("client_key_tmp.pem"))
			if err != nil {
				t.Fatalf("copyFileContents(test.keyFileBefore, testdata.Path(client_key_tmp.pem)): %v", err)
			}
			gotCertBefore, err := tls.LoadX509KeyPair(testdata.Path("client_cert_tmp.pem"), testdata.Path("client_key_tmp.pem"))
			if err != nil {
				t.Fatalf("tls.LoadX509KeyPair(client_cert_tmp.pem, client_key_tmp.pem) failed: %v", err)
			}
			if !cmp.Equal(gotCertBefore, test.wantCertBefore, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("GetCertificates() = %v, want %v", gotCertBefore, test.wantCertBefore)
			}
			time.Sleep(3 * time.Second)
			err = copyFileContents(test.certFileAfter, testdata.Path("client_cert_tmp.pem"))
			if err != nil {
				t.Fatalf("copyFileContents(test.certFileAfter, testdata.Path(client_cert_tmp.pem)): %v", err)
			}
			err = copyFileContents(test.keyFileAfter, testdata.Path("client_key_tmp.pem"))
			if err != nil {
				t.Fatalf("copyFileContents(test.keyFileAfter, testdata.Path(client_key_tmp.pem)): %v", err)
			}
			gotCertAfter, err := tls.LoadX509KeyPair(testdata.Path("client_cert_tmp.pem"), testdata.Path("client_key_tmp.pem"))
			if err != nil {
				t.Fatalf("tls.LoadX509KeyPair(client_cert_tmp.pem, client_key_tmp.pem) failed: %v", err)
			}
			if !cmp.Equal(gotCertAfter, test.wantCertAfter, cmp.AllowUnexported(big.Int{})) {
				t.Errorf("GetCertificates() = %v, want %v", gotCertAfter, test.wantCertAfter)
			}
		})
		if _, err := os.Stat(testdata.Path("client_cert_tmp.pem")); err == nil {
			err := os.Remove(testdata.Path("client_cert_tmp.pem"))
			if err != nil {
				t.Fatalf("os.Remove(testdata.Path(client_cert_tmp.pem)) failed: %v", err)
			}
		}
		if _, err := os.Stat(testdata.Path("client_key_tmp.pem")); err == nil {
			err := os.Remove(testdata.Path("client_key_tmp.pem"))
			if err != nil {
				t.Fatalf("os.Remove(testdata.Path(client_key_tmp.pem)) failed: %v", err)
			}
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
