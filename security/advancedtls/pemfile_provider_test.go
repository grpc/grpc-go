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
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

func (s) TestNewPEMFileProvider(t *testing.T) {
	tests := []struct {
		desc      string
		options   PEMFileProviderOptions
		certFile  string
		keyFile   string
		trustFile string
		wantError bool
	}{
		{
			desc:      "Expect error if no credential files specified",
			options:   PEMFileProviderOptions{},
			wantError: true,
		},
		{
			desc: "Expect error if only certFile is specified",
			options: PEMFileProviderOptions{
				CertFile: testdata.Path("client_cert_1.pem"),
			},
			wantError: true,
		},
		{
			desc: "Should be good if only identity key cert pairs are specified",
			options: PEMFileProviderOptions{
				KeyFile:  testdata.Path("client_key_1.pem"),
				CertFile: testdata.Path("client_cert_1.pem"),
			},
			wantError: false,
		},
		{
			desc: "Should be good if only root certs are specified",
			options: PEMFileProviderOptions{
				TrustFile: testdata.Path("client_trust_cert_1.pem"),
			},
			wantError: false,
		},
		{
			desc: "Should be good if both identity pairs and root certs are specified",
			options: PEMFileProviderOptions{
				KeyFile:   testdata.Path("client_key_1.pem"),
				CertFile:  testdata.Path("client_cert_1.pem"),
				TrustFile: testdata.Path("client_trust_cert_1.pem"),
			},
			wantError: false,
		},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			provider, err := NewPEMFileProvider(test.options)
			if (err != nil) != test.wantError {
				t.Fatalf("NewPEMFileProvider(%v) = %v, want %v", test.options, err, test.wantError)
			}
			if err != nil {
				return
			}
			provider.Close()
		})
	}

}

// This test overwrites the credential reading function used by the watching
// goroutine. It is tested under different stages:
// At stage 0, we force reading function to load clientPeer1 and serverTrust1,
// and see if the credentials are picked up by the watching go routine.
// At stage 1, we force reading function to cause an error. The watching go
// routine should log the error while leaving the credentials unchanged.
// At stage 2, we force reading function to load clientPeer2 and serverTrust2,
// and see if the new credentials are picked up.
func (s) TestWatchingRoutineUpdates(t *testing.T) {
	// Load certificates.
	cs := &certStore{}
	if err := cs.loadCerts(); err != nil {
		t.Fatalf("cs.loadCerts() failed: %v", err)
	}
	tests := []struct {
		desc         string
		options      PEMFileProviderOptions
		wantKmStage0 certprovider.KeyMaterial
		wantKmStage1 certprovider.KeyMaterial
		wantKmStage2 certprovider.KeyMaterial
	}{
		{
			desc: "use identity certs and root certs",
			options: PEMFileProviderOptions{
				CertFile:  "not_empty_cert_file",
				KeyFile:   "not_empty_key_file",
				TrustFile: "not_empty_trust_file",
			},
			wantKmStage0: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}, Roots: cs.serverTrust1},
			wantKmStage1: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}, Roots: cs.serverTrust1},
			wantKmStage2: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer2}, Roots: cs.serverTrust2},
		},
		{
			desc: "use identity certs only",
			options: PEMFileProviderOptions{
				CertFile: "not_empty_cert_file",
				KeyFile:  "not_empty_key_file",
			},
			wantKmStage0: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}},
			wantKmStage1: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer1}},
			wantKmStage2: certprovider.KeyMaterial{Certs: []tls.Certificate{cs.clientPeer2}},
		},
		{
			desc: "use trust certs only",
			options: PEMFileProviderOptions{
				TrustFile: "not_empty_trust_file",
			},
			wantKmStage0: certprovider.KeyMaterial{Roots: cs.serverTrust1},
			wantKmStage1: certprovider.KeyMaterial{Roots: cs.serverTrust1},
			wantKmStage2: certprovider.KeyMaterial{Roots: cs.serverTrust2},
		},
	}
	for _, test := range tests {
		testInterval := 200 * time.Millisecond
		test.options.IdentityInterval = testInterval
		test.options.RootInterval = testInterval
		t.Run(test.desc, func(t *testing.T) {
			stage := &stageInfo{}
			oldReadKeyCertPairFunc := readKeyCertPairFunc
			readKeyCertPairFunc = func(certFile, keyFile string) (tls.Certificate, error) {
				switch stage.read() {
				case 0:
					return cs.clientPeer1, nil
				case 1:
					return tls.Certificate{}, fmt.Errorf("error occurred while reloading")
				case 2:
					return cs.clientPeer2, nil
				default:
					return tls.Certificate{}, fmt.Errorf("test stage not supported")
				}
			}
			defer func() {
				readKeyCertPairFunc = oldReadKeyCertPairFunc
			}()
			oldReadTrustCertFunc := readTrustCertFunc
			readTrustCertFunc = func(trustFile string) (*x509.CertPool, error) {
				switch stage.read() {
				case 0:
					return cs.serverTrust1, nil
				case 1:
					return nil, fmt.Errorf("error occurred while reloading")
				case 2:
					return cs.serverTrust2, nil
				default:
					return nil, fmt.Errorf("test stage not supported")
				}
			}
			defer func() {
				readTrustCertFunc = oldReadTrustCertFunc
			}()
			provider, err := NewPEMFileProvider(test.options)
			if err != nil {
				t.Fatalf("NewPEMFileProvider failed: %v", err)
			}
			defer provider.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			//// ------------------------Stage 0------------------------------------
			// Wait for the refreshing go-routine to pick up the changes.
			time.Sleep(1 * time.Second)
			gotKM, err := provider.KeyMaterial(ctx)
			if !cmp.Equal(*gotKM, test.wantKmStage0, cmp.AllowUnexported(big.Int{}, x509.CertPool{})) {
				t.Fatalf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmStage0)
			}
			// ------------------------Stage 1------------------------------------
			stage.increase()
			// Wait for the refreshing go-routine to pick up the changes.
			time.Sleep(1 * time.Second)
			gotKM, err = provider.KeyMaterial(ctx)
			if !cmp.Equal(*gotKM, test.wantKmStage1, cmp.AllowUnexported(big.Int{}, x509.CertPool{})) {
				t.Fatalf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmStage1)
			}
			//// ------------------------Stage 2------------------------------------
			// Wait for the refreshing go-routine to pick up the changes.
			stage.increase()
			time.Sleep(1 * time.Second)
			gotKM, err = provider.KeyMaterial(ctx)
			if !cmp.Equal(*gotKM, test.wantKmStage2, cmp.AllowUnexported(big.Int{}, x509.CertPool{})) {
				t.Fatalf("provider.KeyMaterial() = %+v, want %+v", *gotKM, test.wantKmStage2)
			}
			stage.reset()
		})
	}
}
