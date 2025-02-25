package spiffe

import (
	"crypto/x509"
	"encoding/pem"
	"io"
	"os"
	"testing"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"google.golang.org/grpc/testdata"
)

var SpiffeTrustBundle map[string]*spiffebundle.Bundle

func TestKnownSpiffeBundle(t *testing.T) {
	bundles, err := LoadSpiffeBundleMap(testdata.Path("spiffe/spiffebundle.json"))
	if err != nil {
		t.Fatalf("Error during parsing: %v", err)
	}
	if len(bundles) != 2 {
		t.Fatal("did not parse correct bundle length")
	}
	if bundles["example.com"] == nil {
		t.Fatal("expected bundle for example.com")
	}
	if bundles["test.example.com"] == nil {
		t.Fatal("expected bundle for test.example.com")
	}

	expectedExampleComCert := loadX509Cert(testdata.Path("spiffe/spiffe_cert.pem"))
	expectedTestExampleComCert := loadX509Cert(testdata.Path("spiffe/server1_spiffe.pem"))
	if !bundles["example.com"].X509Authorities()[0].Equal(expectedExampleComCert) {
		t.Fatalf("expected cert for example.com bundle not correct.")
	}
	if !bundles["test.example.com"].X509Authorities()[0].Equal(expectedTestExampleComCert) {
		t.Fatalf("expected cert for test.example.com bundle not correct.")
	}

}

func loadX509Cert(filePath string) *x509.Certificate {
	certFile, _ := os.Open(filePath)
	certRaw, _ := io.ReadAll(certFile)
	block, _ := pem.Decode([]byte(certRaw))
	if block == nil {
		panic("failed to parse certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		panic("failed to parse certificate: " + err.Error())
	}
	return cert
}

func TestLoadSpiffeBundleMapFailures(t *testing.T) {
	testCases := []struct {
		filePath     string
		expectError  bool
		expectNoX509 bool
	}{
		{
			filePath:    testdata.Path("spiffe/spiffebundle_corrupted_cert.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_malformed.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_wrong_kid.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_wrong_kty.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_wrong_multi_certs.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_wrong_root.json"),
			expectError: true,
		},
		{
			filePath:    testdata.Path("spiffe/spiffebundle_wrong_seq_type.json"),
			expectError: true,
		},
		{
			// SPIFFE Bundles only support a use of x509-svid and jwt-svid. If a
			// use other than this is specified, the parser does not fail, it
			// just doesn't add an x509 authority or jwt authority to the bundle
			filePath:     testdata.Path("spiffe/spiffebundle_wrong_use.json"),
			expectNoX509: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.filePath, func(t *testing.T) {
			bundle, err := LoadSpiffeBundleMap(tc.filePath)
			if tc.expectError && err == nil {
				t.Fatalf("Did not fail but should have.")
			}
			if tc.expectNoX509 && len(bundle["example.com"].X509Authorities()) != 0 {
				t.Fatalf("Did not have empty bundle but should have.")
			}
		})
	}
}
