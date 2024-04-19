// TODO(@gregorycooke) - Remove when only golang 1.19+ is supported
//go:build go1.19

/*
 *
 * Copyright 2021 gRPC authors.
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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"google.golang.org/grpc/security/advancedtls/testdata"
)

func TestX509NameHash(t *testing.T) {
	nameTests := []struct {
		in  pkix.Name
		out string
	}{
		{
			in: pkix.Name{
				Country:      []string{"US"},
				Organization: []string{"Example"},
			},
			out: "9cdd41ff",
		},
		{
			in: pkix.Name{
				Country:      []string{"us"},
				Organization: []string{"example"},
			},
			out: "9cdd41ff",
		},
		{
			in: pkix.Name{
				Country:      []string{"      us"},
				Organization: []string{"example"},
			},
			out: "9cdd41ff",
		},
		{
			in: pkix.Name{
				Country:      []string{"US"},
				Province:     []string{"California"},
				Locality:     []string{"Mountain View"},
				Organization: []string{"BoringSSL"},
			},
			out: "c24414d9",
		},
		{
			in: pkix.Name{
				Country:      []string{"US"},
				Province:     []string{"California"},
				Locality:     []string{"Mountain           View"},
				Organization: []string{"BoringSSL"},
			},
			out: "c24414d9",
		},
		{
			in: pkix.Name{
				SerialNumber: "87f4514475ba0a2b",
			},
			out: "9dc713cd",
		},
		{
			in: pkix.Name{
				Country:            []string{"US"},
				Province:           []string{"California"},
				Locality:           []string{"Mountain View"},
				Organization:       []string{"Google LLC"},
				OrganizationalUnit: []string{"Production", "campus-sln"},
				CommonName:         "Root CA (2021-02-02T07:30:36-08:00)",
			},
			out: "0b35a562",
		},
		{
			in: pkix.Name{
				ExtraNames: []pkix.AttributeTypeAndValue{
					{Type: asn1.ObjectIdentifier{5, 5, 5, 5}, Value: "aaaa"},
				},
			},
			out: "eea339da",
		},
	}
	for _, tt := range nameTests {
		t.Run(tt.in.String(), func(t *testing.T) {
			h := x509NameHash(tt.in.ToRDNSequence())
			if h != tt.out {
				t.Errorf("x509NameHash(%v): Got %v wanted %v", tt.in, h, tt.out)
			}
		})
	}
}

func TestUnsupportedCRLs(t *testing.T) {
	crlBytesSomeReasons := []byte(`-----BEGIN X509 CRL-----
MIIEeDCCA2ACAQEwDQYJKoZIhvcNAQELBQAwQjELMAkGA1UEBhMCVVMxHjAcBgNV
BAoTFUdvb2dsZSBUcnVzdCBTZXJ2aWNlczETMBEGA1UEAxMKR1RTIENBIDFPMRcN
MjEwNDI2MTI1OTQxWhcNMjEwNTA2MTE1OTQwWjCCAn0wIgIRAPOOG3L4VLC7CAAA
AABxQgEXDTIxMDQxOTEyMTgxOFowIQIQUK0UwBZkVdQIAAAAAHFCBRcNMjEwNDE5
MTIxODE4WjAhAhBRIXBJaKoQkQgAAAAAcULHFw0yMTA0MjAxMjE4MTdaMCICEQCv
qQWUq5UxmQgAAAAAcULMFw0yMTA0MjAxMjE4MTdaMCICEQDdv5k1kKwKTQgAAAAA
cUOQFw0yMTA0MjExMjE4MTZaMCICEQDGIEfR8N9sEAgAAAAAcUOWFw0yMTA0MjEx
MjE4MThaMCECEBHgbLXlj5yUCAAAAABxQ/IXDTIxMDQyMTIzMDAyNlowIQIQE1wT
2GGYqKwIAAAAAHFD7xcNMjEwNDIxMjMwMDI5WjAiAhEAo/bSyDjpVtsIAAAAAHFE
txcNMjEwNDIyMjMwMDI3WjAhAhARdCrSrHE0dAgAAAAAcUS/Fw0yMTA0MjIyMzAw
MjhaMCECEHONohfWn3wwCAAAAABxRX8XDTIxMDQyMzIzMDAyOVowIgIRAOYkiUPA
os4vCAAAAABxRYgXDTIxMDQyMzIzMDAyOFowIQIQRNTow5Eg2gEIAAAAAHFGShcN
MjEwNDI0MjMwMDI2WjAhAhBX32dH4/WQ6AgAAAAAcUZNFw0yMTA0MjQyMzAwMjZa
MCICEQDHnUM1vsaP/wgAAAAAcUcQFw0yMTA0MjUyMzAwMjZaMCECEEm5rvmL8sj6
CAAAAABxRxQXDTIxMDQyNTIzMDAyN1owIQIQW16OQs4YQYkIAAAAAHFIABcNMjEw
NDI2MTI1NDA4WjAhAhAhSohpYsJtDQgAAAAAcUgEFw0yMTA0MjYxMjU0MDlaoGkw
ZzAfBgNVHSMEGDAWgBSY0fhuEOvPm+xgnxiQG6DrfQn9KzALBgNVHRQEBAICBngw
NwYDVR0cAQH/BC0wK6AmoCSGImh0dHA6Ly9jcmwucGtpLmdvb2cvR1RTMU8xY29y
ZS5jcmyBAf8wDQYJKoZIhvcNAQELBQADggEBADPBXbxVxMJ1HC7btXExRUpJHUlU
YbeCZGx6zj5F8pkopbmpV7cpewwhm848Fx4VaFFppZQZd92O08daEC6aEqoug4qF
z6ZrOLzhuKfpW8E93JjgL91v0FYN7iOcT7+ERKCwVEwEkuxszxs7ggW6OJYJNvHh
priIdmcPoiQ3ZrIRH0vE3BfUcNXnKFGATWuDkiRI0I4A5P7NiOf+lAuGZet3/eom
0chgts6sdau10GfeUpHUd4f8e93cS/QeLeG16z7LC8vRLstU3m3vrknpZbdGqSia
97w66mqcnQh9V0swZiEnVLmLufaiuDZJ+6nUzSvLqBlb/ei3T/tKV0BoKJA=
-----END X509 CRL-----`)

	crlBytesIndirect := []byte(`-----BEGIN X509 CRL-----
MIIDGjCCAgICAQEwDQYJKoZIhvcNAQELBQAwdjELMAkGA1UEBhMCVVMxEzARBgNV
BAgTCkNhbGlmb3JuaWExFDASBgNVBAoTC1Rlc3RpbmcgTHRkMSowKAYDVQQLEyFU
ZXN0aW5nIEx0ZCBDZXJ0aWZpY2F0ZSBBdXRob3JpdHkxEDAOBgNVBAMTB1Rlc3Qg
Q0EXDTIxMDExNjAyMjAxNloXDTIxMDEyMDA2MjAxNlowgfIwbAIBAhcNMjEwMTE2
MDIyMDE2WjBYMAoGA1UdFQQDCgEEMEoGA1UdHQEB/wRAMD6kPDA6MQwwCgYDVQQG
EwNVU0ExDTALBgNVBAcTBGhlcmUxCzAJBgNVBAoTAnVzMQ4wDAYDVQQDEwVUZXN0
MTAgAgEDFw0yMTAxMTYwMjIwMTZaMAwwCgYDVR0VBAMKAQEwYAIBBBcNMjEwMTE2
MDIyMDE2WjBMMEoGA1UdHQEB/wRAMD6kPDA6MQwwCgYDVQQGEwNVU0ExDTALBgNV
BAcTBGhlcmUxCzAJBgNVBAoTAnVzMQ4wDAYDVQQDEwVUZXN0MqBjMGEwHwYDVR0j
BBgwFoAURJSDWAOfhGCryBjl8dsQjBitl3swCgYDVR0UBAMCAQEwMgYDVR0cAQH/
BCgwJqAhoB+GHWh0dHA6Ly9jcmxzLnBraS5nb29nL3Rlc3QuY3JshAH/MA0GCSqG
SIb3DQEBCwUAA4IBAQBVXX67mr2wFPmEWCe6mf/wFnPl3xL6zNOl96YJtsd7ulcS
TEbdJpaUnWFQ23+Tpzdj/lI2aQhTg5Lvii3o+D8C5r/Jc5NhSOtVJJDI/IQLh4pG
NgGdljdbJQIT5D2Z71dgbq1ocxn8DefZIJjO3jp8VnAm7AIMX2tLTySzD2MpMeMq
XmcN4lG1e4nx+xjzp7MySYO42NRY3LkphVzJhu3dRBYhBKViRJxw9hLttChitJpF
6Kh6a0QzrEY/QDJGhE1VrAD2c5g/SKnHPDVoCWo4ACIICi76KQQSIWfIdp4W/SY3
qsSIp8gfxSyzkJP+Ngkm2DdLjlJQCZ9R0MZP9Xj4
-----END X509 CRL-----`)

	var tests = []struct {
		desc string
		in   []byte
	}{
		{
			desc: "some reasons",
			in:   crlBytesSomeReasons,
		},
		{
			desc: "indirect",
			in:   crlBytesIndirect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			crl, err := parseRevocationList(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := parseCRLExtensions(crl); err == nil {
				t.Error("expected error got ok")
			}
		})
	}
}

func TestCheckCertRevocation(t *testing.T) {
	dummyCrlFile := []byte(`-----BEGIN X509 CRL-----
MIIDGjCCAgICAQEwDQYJKoZIhvcNAQELBQAwdjELMAkGA1UEBhMCVVMxEzARBgNV
BAgTCkNhbGlmb3JuaWExFDASBgNVBAoTC1Rlc3RpbmcgTHRkMSowKAYDVQQLEyFU
ZXN0aW5nIEx0ZCBDZXJ0aWZpY2F0ZSBBdXRob3JpdHkxEDAOBgNVBAMTB1Rlc3Qg
Q0EXDTIxMDExNjAyMjAxNloXDTIxMDEyMDA2MjAxNlowgfIwbAIBAhcNMjEwMTE2
MDIyMDE2WjBYMAoGA1UdFQQDCgEEMEoGA1UdHQEB/wRAMD6kPDA6MQwwCgYDVQQG
EwNVU0ExDTALBgNVBAcTBGhlcmUxCzAJBgNVBAoTAnVzMQ4wDAYDVQQDEwVUZXN0
MTAgAgEDFw0yMTAxMTYwMjIwMTZaMAwwCgYDVR0VBAMKAQEwYAIBBBcNMjEwMTE2
MDIyMDE2WjBMMEoGA1UdHQEB/wRAMD6kPDA6MQwwCgYDVQQGEwNVU0ExDTALBgNV
BAcTBGhlcmUxCzAJBgNVBAoTAnVzMQ4wDAYDVQQDEwVUZXN0MqBjMGEwHwYDVR0j
BBgwFoAURJSDWAOfhGCryBjl8dsQjBitl3swCgYDVR0UBAMCAQEwMgYDVR0cAQH/
BCgwJqAhoB+GHWh0dHA6Ly9jcmxzLnBraS5nb29nL3Rlc3QuY3JshAH/MA0GCSqG
SIb3DQEBCwUAA4IBAQBVXX67mr2wFPmEWCe6mf/wFnPl3xL6zNOl96YJtsd7ulcS
TEbdJpaUnWFQ23+Tpzdj/lI2aQhTg5Lvii3o+D8C5r/Jc5NhSOtVJJDI/IQLh4pG
NgGdljdbJQIT5D2Z71dgbq1ocxn8DefZIJjO3jp8VnAm7AIMX2tLTySzD2MpMeMq
XmcN4lG1e4nx+xjzp7MySYO42NRY3LkphVzJhu3dRBYhBKViRJxw9hLttChitJpF
6Kh6a0QzrEY/QDJGhE1VrAD2c5g/SKnHPDVoCWo4ACIICi76KQQSIWfIdp4W/SY3
qsSIp8gfxSyzkJP+Ngkm2DdLjlJQCZ9R0MZP9Xj4
-----END X509 CRL-----`)
	crl, err := parseRevocationList(dummyCrlFile)
	if err != nil {
		t.Fatalf("parseRevocationList(dummyCrlFile) failed: %v", err)
	}
	crlExt := &CRL{certList: crl}

	var revocationTests = []struct {
		desc    string
		in      x509.Certificate
		revoked revocationStatus
	}{
		{
			desc: "Single revoked",
			in: x509.Certificate{
				Issuer: pkix.Name{
					Country:      []string{"USA"},
					Locality:     []string{"here"},
					Organization: []string{"us"},
					CommonName:   "Test1",
				},
				SerialNumber:          big.NewInt(2),
				CRLDistributionPoints: []string{"test"},
			},
			revoked: RevocationRevoked,
		},
		{
			desc: "Revoked no entry issuer",
			in: x509.Certificate{
				Issuer: pkix.Name{
					Country:      []string{"USA"},
					Locality:     []string{"here"},
					Organization: []string{"us"},
					CommonName:   "Test1",
				},
				SerialNumber:          big.NewInt(3),
				CRLDistributionPoints: []string{"test"},
			},
			revoked: RevocationRevoked,
		},
		{
			desc: "Revoked new entry issuer",
			in: x509.Certificate{
				Issuer: pkix.Name{
					Country:      []string{"USA"},
					Locality:     []string{"here"},
					Organization: []string{"us"},
					CommonName:   "Test2",
				},
				SerialNumber:          big.NewInt(4),
				CRLDistributionPoints: []string{"test"},
			},
			revoked: RevocationRevoked,
		},
		{
			desc: "Single unrevoked",
			in: x509.Certificate{
				Issuer: pkix.Name{
					Country:      []string{"USA"},
					Locality:     []string{"here"},
					Organization: []string{"us"},
					CommonName:   "Test2",
				},
				SerialNumber:          big.NewInt(1),
				CRLDistributionPoints: []string{"test"},
			},
			revoked: RevocationUnrevoked,
		},
		{
			desc: "Single unrevoked Issuer",
			in: x509.Certificate{
				Issuer:                crl.Issuer,
				SerialNumber:          big.NewInt(2),
				CRLDistributionPoints: []string{"test"},
			},
			revoked: RevocationUnrevoked,
		},
	}

	for _, tt := range revocationTests {
		rawIssuer, err := asn1.Marshal(tt.in.Issuer.ToRDNSequence())
		if err != nil {
			t.Fatalf("asn1.Marshal(%v) failed: %v", tt.in.Issuer.ToRDNSequence(), err)
		}
		tt.in.RawIssuer = rawIssuer
		t.Run(tt.desc, func(t *testing.T) {
			rev, err := checkCertRevocation(&tt.in, crlExt)
			if err != nil {
				t.Errorf("checkCertRevocation(%v) err = %v", tt.in.Issuer, err)
			} else if rev != tt.revoked {
				t.Errorf("checkCertRevocation(%v(%v)) returned %v wanted %v",
					tt.in.Issuer, tt.in.SerialNumber, rev, tt.revoked)
			}
		})
	}
}

func makeChain(t *testing.T, name string) []*x509.Certificate {
	t.Helper()

	certChain := make([]*x509.Certificate, 0)

	rest, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("os.ReadFile(%v) failed %v", name, err)
	}
	for len(rest) > 0 {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("ParseCertificate error %v", err)
		}
		t.Logf("Parsed Cert sub = %v iss = %v", c.Subject, c.Issuer)
		certChain = append(certChain, c)
	}
	return certChain
}

func loadCRL(t *testing.T, path string) *CRL {
	crl, err := ReadCRLFile(path)
	if err != nil {
		t.Fatalf("ReadCRLFile(%v) failed err = %v", path, err)
	}
	return crl
}

func TestCachedCRL(t *testing.T) {
	cache, err := lru.New(5)
	if err != nil {
		t.Fatalf("lru.New: err = %v", err)
	}

	tests := []struct {
		desc string
		val  any
		ok   bool
	}{
		{
			desc: "Valid",
			val: &CRL{
				certList: &x509.RevocationList{
					NextUpdate: time.Now().Add(time.Hour),
				}},
			ok: true,
		},
		{
			desc: "Expired",
			val: &CRL{
				certList: &x509.RevocationList{
					NextUpdate: time.Now().Add(-time.Hour),
				}},
			ok: false,
		},
		{
			desc: "Wrong Type",
			val:  "string",
			ok:   false,
		},
		{
			desc: "Empty",
			val:  nil,
			ok:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if tt.val != nil {
				cache.Add(hex.EncodeToString([]byte(tt.desc)), tt.val)
			}
			_, ok := cachedCrl([]byte(tt.desc), cache)
			if tt.ok != ok {
				t.Errorf("Cache ok error expected %v vs %v", tt.ok, ok)
			}
		})
	}
}

func TestGetIssuerCRLCache(t *testing.T) {
	cache, err := lru.New(5)
	if err != nil {
		t.Fatalf("lru.New: err = %v", err)
	}

	tests := []struct {
		desc      string
		rawIssuer []byte
		certs     []*x509.Certificate
	}{
		{
			desc:      "Valid",
			rawIssuer: makeChain(t, testdata.Path("crl/unrevoked.pem"))[1].RawIssuer,
			certs:     makeChain(t, testdata.Path("crl/unrevoked.pem")),
		},
		{
			desc:      "Unverified",
			rawIssuer: makeChain(t, testdata.Path("crl/unrevoked.pem"))[1].RawIssuer,
		},
		{
			desc:      "Not Found",
			rawIssuer: []byte("not_found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			cache.Purge()
			_, err := fetchIssuerCRL(tt.rawIssuer, tt.certs, RevocationConfig{
				RootDir: testdata.Path("."),
				Cache:   cache,
			})
			if err == nil && cache.Len() == 0 {
				t.Error("Verified CRL not added to cache")
			}
			if err != nil && cache.Len() != 0 {
				t.Error("Unverified CRL added to cache")
			}
		})
	}
}

func TestVerifyCrl(t *testing.T) {
	tamperedSignature := loadCRL(t, testdata.Path("crl/1.crl"))
	// Change the signature so it won't verify
	tamperedSignature.certList.Signature[0]++
	tamperedContent := loadCRL(t, testdata.Path("crl/provider_crl_empty.pem"))
	// Change the content so it won't find a match
	tamperedContent.rawIssuer[0]++

	verifyTests := []struct {
		desc    string
		crl     *CRL
		certs   []*x509.Certificate
		cert    *x509.Certificate
		errWant string
	}{
		{
			desc:    "Pass intermediate",
			crl:     loadCRL(t, testdata.Path("crl/1.crl")),
			certs:   makeChain(t, testdata.Path("crl/unrevoked.pem")),
			cert:    makeChain(t, testdata.Path("crl/unrevoked.pem"))[1],
			errWant: "",
		},
		{
			desc:    "Pass leaf",
			crl:     loadCRL(t, testdata.Path("crl/2.crl")),
			certs:   makeChain(t, testdata.Path("crl/unrevoked.pem")),
			cert:    makeChain(t, testdata.Path("crl/unrevoked.pem"))[2],
			errWant: "",
		},
		{
			desc:    "Fail wrong cert chain",
			crl:     loadCRL(t, testdata.Path("crl/3.crl")),
			certs:   makeChain(t, testdata.Path("crl/unrevoked.pem")),
			cert:    makeChain(t, testdata.Path("crl/revokedInt.pem"))[1],
			errWant: "No certificates matched",
		},
		{
			desc:    "Fail no certs",
			crl:     loadCRL(t, testdata.Path("crl/1.crl")),
			certs:   []*x509.Certificate{},
			cert:    makeChain(t, testdata.Path("crl/unrevoked.pem"))[1],
			errWant: "No certificates matched",
		},
		{
			desc:    "Fail Tampered signature",
			crl:     tamperedSignature,
			certs:   makeChain(t, testdata.Path("crl/unrevoked.pem")),
			cert:    makeChain(t, testdata.Path("crl/unrevoked.pem"))[1],
			errWant: "verification failure",
		},
		{
			desc:    "Fail Tampered content",
			crl:     tamperedContent,
			certs:   makeChain(t, testdata.Path("crl/provider_client_trust_cert.pem")),
			cert:    makeChain(t, testdata.Path("crl/provider_client_trust_cert.pem"))[0],
			errWant: "No certificates",
		},
		{
			desc:    "Fail CRL by malicious CA",
			crl:     loadCRL(t, testdata.Path("crl/provider_malicious_crl_empty.pem")),
			certs:   makeChain(t, testdata.Path("crl/provider_client_trust_cert.pem")),
			cert:    makeChain(t, testdata.Path("crl/provider_client_trust_cert.pem"))[0],
			errWant: "verification error",
		},
		{
			desc:    "Fail KeyUsage without cRLSign bit",
			crl:     loadCRL(t, testdata.Path("crl/provider_malicious_crl_empty.pem")),
			certs:   makeChain(t, testdata.Path("crl/provider_malicious_client_trust_cert.pem")),
			cert:    makeChain(t, testdata.Path("crl/provider_malicious_client_trust_cert.pem"))[0],
			errWant: "certificate can't be used",
		},
	}

	for _, tt := range verifyTests {
		t.Run(tt.desc, func(t *testing.T) {
			err := verifyCRL(tt.crl, tt.certs)
			switch {
			case tt.errWant == "" && err != nil:
				t.Errorf("Valid CRL did not verify err = %v", err)
			case tt.errWant != "" && err == nil:
				t.Error("Invalid CRL verified")
			case tt.errWant != "" && !strings.Contains(err.Error(), tt.errWant):
				t.Errorf("fetchIssuerCRL(_, %v, %v, _) = %v; want Contains(%v)", tt.cert.RawIssuer, tt.certs, err, tt.errWant)
			}
		})
	}
}

func TestRevokedCert(t *testing.T) {
	revokedIntChain := makeChain(t, testdata.Path("crl/revokedInt.pem"))
	revokedLeafChain := makeChain(t, testdata.Path("crl/revokedLeaf.pem"))
	validChain := makeChain(t, testdata.Path("crl/unrevoked.pem"))
	cache, err := lru.New(5)
	rawCRLs := make([][]byte, 6)
	for i := 1; i <= 6; i++ {
		rawCRL, err := os.ReadFile(testdata.Path(fmt.Sprintf("crl/%d.crl", i)))
		if err != nil {
			t.Fatalf("readFile(%v) failed err = %v", fmt.Sprintf("crl/%d.crl", i), err)
		}
		rawCRLs = append(rawCRLs, rawCRL)
	}
	cRLProvider := NewStaticCRLProvider(rawCRLs)
	if err != nil {
		t.Fatalf("lru.New: err = %v", err)
	}

	var revocationTests = []struct {
		desc              string
		in                tls.ConnectionState
		revoked           bool
		allowUndetermined bool
	}{
		{
			desc:    "Single unrevoked",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{validChain}},
			revoked: false,
		},
		{
			desc:    "Single revoked intermediate",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{revokedIntChain}},
			revoked: true,
		},
		{
			desc:    "Single revoked leaf",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{revokedLeafChain}},
			revoked: true,
		},
		{
			desc:    "Multi one revoked",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{validChain, revokedLeafChain}},
			revoked: false,
		},
		{
			desc:    "Multi revoked",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{revokedLeafChain, revokedIntChain}},
			revoked: true,
		},
		{
			desc:    "Multi unrevoked",
			in:      tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{validChain, validChain}},
			revoked: false,
		},
		{
			desc: "Undetermined revoked",
			in: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{
				{&x509.Certificate{CRLDistributionPoints: []string{"test"}}},
			}},
			revoked: true,
		},
		{
			desc: "Undetermined allowed",
			in: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{
				{&x509.Certificate{CRLDistributionPoints: []string{"test"}}},
			}},
			revoked:           false,
			allowUndetermined: true,
		},
	}

	for _, tt := range revocationTests {
		t.Run(fmt.Sprintf("%v with x509 crl hash dir", tt.desc), func(t *testing.T) {
			err := checkRevocation(tt.in, RevocationConfig{
				RootDir:           testdata.Path("crl"),
				AllowUndetermined: tt.allowUndetermined,
				Cache:             cache,
			})
			t.Logf("checkRevocation err = %v", err)
			if tt.revoked && err == nil {
				t.Error("Revoked certificate chain was allowed")
			} else if !tt.revoked && err != nil {
				t.Error("Unrevoked certificate not allowed")
			}
		})
		t.Run(fmt.Sprintf("%v with static provider", tt.desc), func(t *testing.T) {
			err := checkRevocation(tt.in, RevocationConfig{
				AllowUndetermined: tt.allowUndetermined,
				CRLProvider:       cRLProvider,
			})
			t.Logf("checkRevocation err = %v", err)
			if tt.revoked && err == nil {
				t.Error("Revoked certificate chain was allowed")
			} else if !tt.revoked && err != nil {
				t.Error("Unrevoked certificate not allowed")
			}
		})
	}
}

func setupTLSConn(t *testing.T) (net.Listener, *x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	templ := x509.Certificate{
		SerialNumber:          big.NewInt(5),
		BasicConstraintsValid: true,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		Subject:               pkix.Name{CommonName: "test-cert"},
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("::1")},
		CRLDistributionPoints: []string{"http://static.corp.google.com/crl/campus-sln/borg"},
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey failed err = %v", err)
	}
	rawCert, err := x509.CreateCertificate(rand.Reader, &templ, &templ, key.Public(), key)
	if err != nil {
		t.Fatalf("x509.CreateCertificate failed err = %v", err)
	}
	cert, err := x509.ParseCertificate(rawCert)
	if err != nil {
		t.Fatalf("x509.ParseCertificate failed err = %v", err)
	}

	srvCfg := tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{cert.Raw},
				PrivateKey:  key,
			},
		},
	}
	l, err := tls.Listen("tcp6", "[::1]:0", &srvCfg)
	if err != nil {
		t.Fatalf("tls.Listen failed err = %v", err)
	}
	return l, cert, key
}

// TestVerifyConnection will setup a client/server connection and check revocation in the real TLS dialer
func TestVerifyConnection(t *testing.T) {
	lis, cert, key := setupTLSConn(t)
	defer func() {
		lis.Close()
	}()

	var handshakeTests = []struct {
		desc    string
		revoked []pkix.RevokedCertificate
		success bool
	}{
		{
			desc:    "Empty CRL",
			revoked: []pkix.RevokedCertificate{},
			success: true,
		},
		{
			desc: "Revoked Cert",
			revoked: []pkix.RevokedCertificate{
				{
					SerialNumber:   cert.SerialNumber,
					RevocationTime: time.Now(),
				},
			},
			success: false,
		},
	}
	for _, tt := range handshakeTests {
		t.Run(tt.desc, func(t *testing.T) {
			// Accept one connection.
			go func() {
				conn, err := lis.Accept()
				if err != nil {
					t.Errorf("tls.Accept failed err = %v", err)
				} else {
					conn.Write([]byte("Hello, World!"))
					conn.Close()
				}
			}()

			dir, err := os.MkdirTemp("", "crl_dir")
			if err != nil {
				t.Fatalf("os.MkdirTemp failed err = %v", err)
			}
			defer os.RemoveAll(dir)

			template := &x509.RevocationList{
				RevokedCertificates: tt.revoked,
				ThisUpdate:          time.Now(),
				NextUpdate:          time.Now().Add(time.Hour),
				Number:              big.NewInt(1),
			}
			crl, err := x509.CreateRevocationList(rand.Reader, template, cert, key)
			if err != nil {
				t.Fatalf("templ.CreateRevocationList failed err = %v", err)
			}

			err = os.WriteFile(path.Join(dir, fmt.Sprintf("%s.r0", x509NameHash(cert.Subject.ToRDNSequence()))), crl, 0777)
			if err != nil {
				t.Fatalf("os.WriteFile failed err = %v", err)
			}

			cp := x509.NewCertPool()
			cp.AddCert(cert)
			cliCfg := tls.Config{
				RootCAs: cp,
				VerifyConnection: func(cs tls.ConnectionState) error {
					return checkRevocation(cs, RevocationConfig{RootDir: dir})
				},
			}
			conn, err := tls.Dial(lis.Addr().Network(), lis.Addr().String(), &cliCfg)
			t.Logf("tls.Dial err = %v", err)
			if tt.success && err != nil {
				t.Errorf("Expected success got err = %v", err)
			}
			if !tt.success && err == nil {
				t.Error("Expected error, but got success")
			}
			if err == nil {
				conn.Close()
			}
		})
	}
}

func TestIssuerNonPrintableString(t *testing.T) {
	rawIssuer, err := hex.DecodeString("300c310a300806022a030c023a29")
	if err != nil {
		t.Fatalf("failed to decode issuer: %s", err)
	}
	_, err = fetchCRLOpenSSLHashDir(rawIssuer, RevocationConfig{RootDir: testdata.Path("crl")})
	if err != nil {
		t.Fatalf("fetchCRL failed: %s", err)
	}
}

// TestCRLCacheExpirationReloading tests the basic expiration and reloading of a
// cached CRL. The setup places an empty CRL in the cache, and a corresponding
// CRL with a revocation in the CRL directory. We then validate the certificate
// to verify that the certificate is not revoked.  Then, we modify the
// NextUpdate time to be in the past so that when we next check for revocation,
// the existing cache entry should be seen as expired, and the CRL in the
// directory showing `revokedInt.pem` as revoked will be loaded, resulting in
// the check returning `RevocationRevoked`.
func TestCRLCacheExpirationReloading(t *testing.T) {
	cache, err := lru.New(5)
	if err != nil {
		t.Fatalf("Creating cache failed")
	}

	var certs = makeChain(t, testdata.Path("crl/revokedInt.pem"))
	// Certs[1] has the same issuer as the revoked cert
	rawIssuer := certs[1].RawIssuer

	// `3.crl`` revokes `revokedInt.pem`
	crl := loadCRL(t, testdata.Path("crl/3.crl"))
	// Modify the crl so that the cert is NOT revoked and add it to the cache
	crl.certList.RevokedCertificates = nil
	crl.certList.NextUpdate = time.Now().Add(time.Hour)
	cache.Add(hex.EncodeToString(rawIssuer), crl)
	var cfg = RevocationConfig{RootDir: testdata.Path("crl"), Cache: cache}
	revocationStatus := checkChain(certs, cfg)
	if revocationStatus != RevocationUnrevoked {
		t.Fatalf("Certificate check should be RevocationUnrevoked, was %v", revocationStatus)
	}

	// Modify the entry in the cache so that the cache will be refreshed
	crl.certList.NextUpdate = time.Now()
	cache.Add(hex.EncodeToString(rawIssuer), crl)

	revocationStatus = checkChain(certs, cfg)
	if revocationStatus != RevocationRevoked {
		t.Fatalf("A certificate should have been `RevocationRevoked` but was %v", revocationStatus)
	}
}
