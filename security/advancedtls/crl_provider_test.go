/*
 *
 * Copyright 2023 gRPC authors.
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
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/security/advancedtls/testdata"
)

const nonCRLFilesUnderCRLDirectory = 5

func TestStaticCRLProvider(t *testing.T) {
	p := MakeStaticCRLProvider()
	for i := 1; i <= 6; i++ {
		crl := loadCRL(t, testdata.Path(fmt.Sprintf("crl/%d.crl", i)))
		p.AddCRL(crl)
	}

	tests := []struct {
		desc        string
		certs       []*x509.Certificate
		expectNoCRL bool
	}{
		{
			desc:  "Unrevoked chain",
			certs: makeChain(t, testdata.Path("crl/unrevoked.pem")),
		},
		{
			desc:  "Revoked Intermediate chain",
			certs: makeChain(t, testdata.Path("crl/revokedInt.pem")),
		},
		{
			desc:  "Revoked leaf chain",
			certs: makeChain(t, testdata.Path("crl/revokedLeaf.pem")),
		},
		{
			desc:        "Chain with no CRL for issuer",
			certs:       makeChain(t, testdata.Path("client_cert_1.pem")),
			expectNoCRL: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			for _, c := range tt.certs {
				crl, err := p.CRL(c)
				if err != nil {
					t.Fatalf("Expected error fetch from provider: %v", err)
				}
				if crl == nil && !tt.expectNoCRL {
					t.Fatalf("CRL is unexpectedly nil")
				}
			}
		})
	}
}

func TestFileWatcherCRLProviderConfig(t *testing.T) {
	if _, err := MakeFileWatcherCRLProvider(Options{}); err == nil {
		t.Fatalf("Empty Options should not be allowed")
	}
	if _, err := MakeFileWatcherCRLProvider(Options{CRLDirectory: "I_do_not_exist"}); err == nil {
		t.Fatalf("CRLDirectory must exist")
	}
	if defaultProvider, err := MakeFileWatcherCRLProvider(Options{CRLDirectory: testdata.Path("crl/provider")}); err == nil {
		if defaultProvider.opts.RefreshDuration != defaultCRLRefreshDuration {
			t.Fatalf("RefreshDuration is not properly updated by validate() func")
		}
		defaultProvider.Close()
	} else {
		t.Fatal("Unexpected error:", err)
	}

	customCallback := func(err error) {
		fmt.Printf("Custom error message: %v", err)
	}
	regularProvider, err := MakeFileWatcherCRLProvider(Options{
		CRLDirectory:               testdata.Path("crl"),
		RefreshDuration:            5 * time.Second,
		cRLReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating regular FileWatcherCRLProvider:", err)
	}
	regularProvider.Close()
}

func TestFileWatcherCRLProvider(t *testing.T) {
	// testdata.Path("crl") contains 5 non-crl files.
	failedCRlsCounter := 0
	customCallback := func(err error) {
		failedCRlsCounter++
	}
	p, err := MakeFileWatcherCRLProvider(Options{
		CRLDirectory:               testdata.Path("crl"),
		RefreshDuration:            5 * time.Second,
		cRLReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating FileWatcherCRLProvider:", err)
	}
	p.scanCRLDirectory()
	tests := []struct {
		desc        string
		certs       []*x509.Certificate
		expectNoCRL bool
	}{
		{
			desc:  "Unrevoked chain",
			certs: makeChain(t, testdata.Path("crl/unrevoked.pem")),
		},
		{
			desc:  "Revoked Intermediate chain",
			certs: makeChain(t, testdata.Path("crl/revokedInt.pem")),
		},
		{
			desc:  "Revoked leaf chain",
			certs: makeChain(t, testdata.Path("crl/revokedLeaf.pem")),
		},
		{
			desc:        "Chain with no CRL for issuer",
			certs:       makeChain(t, testdata.Path("client_cert_1.pem")),
			expectNoCRL: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			for _, c := range tt.certs {
				crl, err := p.CRL(c)
				if err != nil {
					t.Fatalf("Expected error fetch from provider: %v", err)
				}
				if crl == nil && !tt.expectNoCRL {
					t.Fatalf("CRL is unexpectedly nil")
				}
			}
		})
	}
	if failedCRlsCounter < nonCRLFilesUnderCRLDirectory {
		t.Fatalf("Number of callback execution is smaller then number of non-CRL files: got %v, want at least %v", failedCRlsCounter, nonCRLFilesUnderCRLDirectory)
	}
	p.Close()
}
