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
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

const nonCRLFilesUnderCRLDirectory = 5

// TestStaticCRLProvider tests how StaticCRLProvider handles the major four
// cases for CRL checks. It loads the CRLs under crl directory, constructs
// unrevoked, revoked leaf, and revoked intermediate chains, as well as a chain
// without CRL for issuer, and checks that it’s correctly processed.
func TestStaticCRLProvider(t *testing.T) {
	rawCRLs := make([][]byte, 6)
	for i := 1; i <= 6; i++ {
		rawCRL, err := os.ReadFile(testdata.Path(fmt.Sprintf("crl/%d.crl", i)))
		if err != nil {
			t.Fatalf("readFile(%v) failed err = %v", fmt.Sprintf("crl/%d.crl", i), err)
		}
		rawCRLs = append(rawCRLs, rawCRL)
	}
	p := MakeStaticCRLProvider(rawCRLs)
	// Each test data entry contains a description of a certificate chain,
	// certificate chain itself, and if CRL is not expected to be found.
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

// TestFileWatcherCRLProviderConfig checks creation of FileWatcherCRLProvider,
// and the validation of Options configuration. The configurations include empty
// one, non existing CRLDirectory, invalid RefreshDuration, and the correct one.
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
		crlReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating regular FileWatcherCRLProvider:", err)
	}
	regularProvider.Close()
}

// TestFileWatcherCRLProvider tests how FileWatcherCRLProvider handles the major
// four cases for CRL checks. It scans the CRLs under crl directory to populate
// the in-memory storage. Then we construct unrevoked, revoked leaf, and revoked
// intermediate chains, as well as a chain without CRL for issuer, and check
// that it’s correctly processed. Additionally, we also check if number of
// invocations of custom callback is correct.
func TestFileWatcherCRLProvider(t *testing.T) {
	// testdata.Path("crl") contains 5 non-crl files.
	nonCRLFilesSet := make(map[string]struct{})
	customCallback := func(err error) {
		nonCRLFilesSet[err.Error()] = struct{}{}
	}
	p, err := MakeFileWatcherCRLProvider(Options{
		CRLDirectory:               testdata.Path("crl"),
		RefreshDuration:            5 * time.Second,
		crlReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating FileWatcherCRLProvider:", err)
	}
	p.ScanCRLDirectory()

	// Each test data entry contains a description of a certificate chain,
	// certificate chain itself, and if CRL is not expected to be found.
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
	if len(nonCRLFilesSet) < nonCRLFilesUnderCRLDirectory {
		t.Fatalf("Number of callback executions: got %v, want %v", len(nonCRLFilesSet), nonCRLFilesUnderCRLDirectory)
	}
	p.Close()
}

// TestFileWatcherCRLProviderDirectoryScan tests how FileWatcherCRLProvider
// handles different contents of Options.CRLDirectory
// We update the content with new (correct and incorrect) CRL files and check if
// in-memory storage was properly updated
func TestFileWatcherCRLProviderDirectoryScan(t *testing.T) {
	sourcePath := testdata.Path("crl")
	targetPath := testdata.Path("crl/provider/filewatcher")
	p, err := MakeFileWatcherCRLProvider(Options{
		CRLDirectory:    targetPath,
		RefreshDuration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating FileWatcherCRLProvider:", err)
	}

	tests := []struct {
		desc            string
		fileNames       []string
		expectedEntries int
	}{
		{
			desc:            "Empty dir",
			fileNames:       []string{},
			expectedEntries: 0,
		},
		{
			desc:            "Simple addition",
			fileNames:       []string{"1.crl"},
			expectedEntries: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			copyFiles(sourcePath, targetPath, tt.fileNames, t)
			p.ScanCRLDirectory()
			if diff := cmp.Diff(len(p.crls), tt.expectedEntries); diff != "" {
				t.Errorf("Expected number of entries in the map do not match\ndiff (-got +want):\n%s", diff)
			}
		})
	}

	p.Close()
}

func copyFiles(sourcePath string, targetPath string, fileNames []string, t *testing.T) {
	targetDir, err := os.Open(targetPath)
	if err != nil {
		t.Fatalf("Can't open dir %v: %v", targetPath, err)
	}
	defer targetDir.Close()
	names, err := targetDir.Readdirnames(-1)
	if err != nil {
		t.Fatalf("Can't read dir %v: %v", targetPath, err)
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(testdata.Path(targetPath), name))
		if err != nil {
			t.Fatalf("Can't remove file %v: %v", name, err)
		}
	}
	for _, fileName := range fileNames {
		destinationPath := filepath.Join(targetPath, fileName)

		sourceFile, err := os.Open(filepath.Join(sourcePath, fileName))
		if err != nil {
			t.Fatalf("Can't open file %v: %v", fileName, err)
		}
		defer sourceFile.Close()

		destinationFile, err := os.Create(destinationPath)
		if err != nil {
			t.Fatalf("Can't create file %v: %v", destinationFile, err)
		}
		defer destinationFile.Close()

		_, err = io.Copy(destinationFile, sourceFile)
		if err != nil {
			t.Fatalf("Can't copy file %v to %v: %v", sourceFile, destinationFile, err)
		}
	}
}
