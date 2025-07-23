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
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

// TestStaticCRLProvider tests how StaticCRLProvider handles the major four
// cases for CRL checks. It loads the CRLs under crl directory, constructs
// unrevoked, revoked leaf, and revoked intermediate chains, as well as a chain
// without CRL for issuer, and checks that it’s correctly processed.
func (s) TestStaticCRLProvider(t *testing.T) {
	rawCRLs := make([][]byte, 6)
	for i := 1; i <= 6; i++ {
		rawCRL, err := os.ReadFile(testdata.Path(fmt.Sprintf("crl/%d.crl", i)))
		if err != nil {
			t.Fatalf("readFile(%v) failed err = %v", fmt.Sprintf("crl/%d.crl", i), err)
		}
		rawCRLs = append(rawCRLs, rawCRL)
	}
	p := NewStaticCRLProvider(rawCRLs)

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
// and the validation of FileWatcherOptions configuration. The configurations include empty
// one, non existing CRLDirectory, invalid RefreshDuration, and the correct one.
func (s) TestFileWatcherCRLProviderConfig(t *testing.T) {
	if _, err := NewFileWatcherCRLProvider(FileWatcherOptions{}); err == nil {
		t.Fatalf("Empty FileWatcherOptions should not be allowed")
	}
	if _, err := NewFileWatcherCRLProvider(FileWatcherOptions{CRLDirectory: "I_do_not_exist"}); err == nil {
		t.Fatalf("CRLDirectory must exist")
	}
	defaultProvider, err := NewFileWatcherCRLProvider(FileWatcherOptions{CRLDirectory: testdata.Path("crl")})
	if err != nil {
		t.Fatal("Unexpected error:", err)
	}
	if defaultProvider.opts.RefreshDuration != defaultCRLRefreshDuration {
		t.Fatalf("RefreshDuration for defaultCRLRefreshDuration case is not properly updated by validate() func")
	}
	defaultProvider.Close()
	tooFastRefreshProvider, err := NewFileWatcherCRLProvider(FileWatcherOptions{
		CRLDirectory:    testdata.Path("crl"),
		RefreshDuration: 5 * time.Second,
	})
	if err != nil {
		t.Fatal("Unexpected error:", err)
	}
	if tooFastRefreshProvider.opts.RefreshDuration != minCRLRefreshDuration {
		t.Fatalf("RefreshDuration for minCRLRefreshDuration case is not properly updated by validate() func")
	}
	tooFastRefreshProvider.Close()

	customCallback := func(err error) {
		t.Logf("Custom error message: %v", err)
	}
	regularProvider, err := NewFileWatcherCRLProvider(FileWatcherOptions{
		CRLDirectory:               testdata.Path("crl"),
		RefreshDuration:            2 * time.Hour,
		CRLReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating regular FileWatcherCRLProvider:", err)
	}
	if regularProvider.opts.RefreshDuration != 2*time.Hour {
		t.Fatalf("Valid refreshDuration was incorrectly updated by validate() func")
	}
	regularProvider.Close()
}

// TestFileWatcherCRLProvider tests how FileWatcherCRLProvider handles the major
// four cases for CRL checks. It scans the CRLs under crl directory to populate
// the in-memory storage. Then we construct unrevoked, revoked leaf, and revoked
// intermediate chains, as well as a chain without CRL for issuer, and check
// that it’s correctly processed. Additionally, we also check if number of
// invocations of custom callback is correct.
func (s) TestFileWatcherCRLProvider(t *testing.T) {
	const nonCRLFilesUnderCRLDirectory = 17
	nonCRLFilesSet := make(map[string]struct{})
	customCallback := func(err error) {
		if strings.Contains(err.Error(), "BUILD") {
			return
		}
		nonCRLFilesSet[err.Error()] = struct{}{}
	}
	p, err := NewFileWatcherCRLProvider(FileWatcherOptions{
		CRLDirectory:               testdata.Path("crl"),
		RefreshDuration:            1 * time.Hour,
		CRLReloadingFailedCallback: customCallback,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating FileWatcherCRLProvider:", err)
	}

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
	p.Close()
	if diff := cmp.Diff(len(nonCRLFilesSet), nonCRLFilesUnderCRLDirectory); diff != "" {
		t.Errorf("Unexpected number Number of callback executions\ndiff (-got +want):\n%s", diff)
	}
}

// TestFileWatcherCRLProviderDirectoryScan tests how FileWatcherCRLProvider
// handles different contents of FileWatcherOptions.CRLDirectory.
// We update the content with various (correct and incorrect) CRL files and
// check if in-memory storage was properly updated. Please note that the same
// instance of FileWatcherCRLProvider is used for the whole test so test cases
// are not independent from each other.
func (s) TestFileWatcherCRLProviderDirectoryScan(t *testing.T) {
	sourcePath := testdata.Path("crl")
	targetPath := createTmpDir(t)
	defer os.RemoveAll(targetPath)
	p, err := NewFileWatcherCRLProvider(FileWatcherOptions{
		CRLDirectory:    targetPath,
		RefreshDuration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatal("Unexpected error while creating FileWatcherCRLProvider:", err)
	}

	// Each test data entry contains a description of CRL directory content
	// (including the expected number of entries in the FileWatcherCRLProvider
	// map), the name of the files to be copied there before executing the test
	// case, and information regarding whether a specific certificate is expected
	// to be found in the map.
	tests := []struct {
		desc          string
		crlFileNames  []string
		certFileNames []struct {
			fileName string
			expected bool
		}
	}{
		{
			desc:         "Simple addition (1 map entry)",
			crlFileNames: []string{"1.crl"},
			certFileNames: []struct {
				fileName string
				expected bool
			}{
				{"crl/unrevoked.pem", true},
			},
		},
		{
			desc:         "Addition and deletion (2 map entries)",
			crlFileNames: []string{"3.crl", "5.crl"},
			certFileNames: []struct {
				fileName string
				expected bool
			}{
				{"crl/revokedInt.pem", true},
				{"crl/revokedLeaf.pem", true},
				{"crl/unrevoked.pem", false},
			},
		},
		{
			desc:         "Addition and a corrupt file (3 map entries)",
			crlFileNames: []string{"1.crl", "README.md"},
			certFileNames: []struct {
				fileName string
				expected bool
			}{
				{"crl/revokedInt.pem", true},
				{"crl/revokedLeaf.pem", true},
				{"crl/unrevoked.pem", true},
			}},
		{
			desc:         "Full deletion (0 map entries)",
			crlFileNames: []string{},
			certFileNames: []struct {
				fileName string
				expected bool
			}{
				{"crl/revokedInt.pem", false},
				{"crl/revokedLeaf.pem", false},
				{"crl/unrevoked.pem", false},
			}},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			copyFiles(sourcePath, targetPath, tt.crlFileNames, t)
			p.scanCRLDirectory()
			for _, certFileName := range tt.certFileNames {
				c := makeChain(t, testdata.Path(certFileName.fileName))[0]
				crl, err := p.CRL(c)
				if err != nil {
					t.Errorf("Cannot fetch CRL from provider: %v", err)
				}
				if crl == nil && certFileName.expected {
					t.Errorf("CRL is unexpectedly nil")
				}
				if crl != nil && !certFileName.expected {
					t.Errorf("CRL is unexpectedly not nil")
				}
			}
		})
	}
	p.Close()
}

func copyFiles(sourcePath string, targetPath string, fileNames []string, t *testing.T) {
	t.Helper()
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
		err = os.RemoveAll(filepath.Join(targetPath, name))
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

func createTmpDir(t *testing.T) string {
	t.Helper()

	// Create a temp directory. Passing an empty string for the first argument
	// uses the system temp directory.
	dir, err := os.MkdirTemp("", "filewatcher*")
	if err != nil {
		t.Fatalf("os.MkdirTemp() failed: %v", err)
	}
	t.Logf("Using tmpdir: %s", dir)
	return dir
}
