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

package revocation

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/cryptobyte"
	cbasn1 "golang.org/x/crypto/cryptobyte/asn1"
)

// Cache is an interface to cache CRL files.
// The cache implementation must be concurrency safe.
// A fixed size lru cache from golang-lru is recommended.
type Cache interface {
	// Add adds a value to the cache.
	Add(key, value any) bool
	// Get looks up a key's value from the cache.
	Get(key any) (value any, ok bool)
}

// RevocationConfig contains options for CRL lookup.
type RevocationConfig struct {
	// RootDir is the directory to search for CRL files.
	// Directory format must match OpenSSL X509_LOOKUP_hash_dir(3).
	// Deprecated: use CRLProvider instead.
	RootDir string
	// AllowUndetermined controls if certificate chains with RevocationUndetermined
	// revocation status are allowed to complete.
	AllowUndetermined bool
	// Cache will store CRL files if not nil, otherwise files are reloaded for every lookup.
	// Only used for caching CRLs when using the RootDir setting.
	// Deprecated: use CRLProvider instead.
	Cache Cache
	// CRLProvider is an alternative to using RootDir directly for the
	// X509_LOOKUP_hash_dir approach to CRL files. If set, the CRLProvider's CRL
	// function will be called when looking up and fetching CRLs during the
	// handshake.
	CRLProvider CRLProvider
}

// RevocationStatus is the revocation status for a certificate or chain.
type RevocationStatus int

const (
	// RevocationUndetermined means we couldn't find or verify a CRL for the cert.
	RevocationUndetermined RevocationStatus = iota
	// RevocationUnrevoked means we found the CRL for the cert and the cert is not revoked.
	RevocationUnrevoked
	// RevocationRevoked means we found the CRL and the cert is revoked.
	RevocationRevoked
)

func (s RevocationStatus) String() string {
	return [...]string{"RevocationUndetermined", "RevocationUnrevoked", "RevocationRevoked"}[s]
}

// CRL contains a pkix.CertificateList and parsed extensions that aren't
// provided by the golang CRL parser.
// All CRLs should be loaded using NewCRL() for bytes directly or ReadCRLFile()
// to read directly from a filepath
type CRL struct {
	CertList *x509.RevocationList
	// RFC5280, 5.2.1, all conforming CRLs must have a AKID with the ID method.
	AuthorityKeyID []byte
	RawIssuer      []byte
}

// NewCRL constructs new CRL from the provided byte array.
func NewCRL(b []byte) (*CRL, error) {
	crl, err := parseRevocationList(b)
	if err != nil {
		return nil, fmt.Errorf("fail to parse CRL: %v", err)
	}
	crlExt, err := parseCRLExtensions(crl)
	if err != nil {
		return nil, fmt.Errorf("fail to parse CRL extensions: %v", err)
	}
	crlExt.RawIssuer, err = extractCRLIssuer(b)
	if err != nil {
		return nil, fmt.Errorf("fail to extract CRL issuer failed err= %v", err)
	}
	return crlExt, nil
}

// ReadCRLFile reads a file from the provided path, and returns constructed CRL
// struct from it.
func ReadCRLFile(path string) (*CRL, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read file from provided path %q: %v", path, err)
	}
	crl, err := NewCRL(b)
	if err != nil {
		return nil, fmt.Errorf("cannot construct CRL from file %q: %v", path, err)
	}
	return crl, nil
}

const tagDirectoryName = 4

var (
	// RFC5280, 5.2.4 id-ce-deltaCRLIndicator OBJECT IDENTIFIER ::= { id-ce 27 }
	oidDeltaCRLIndicator = asn1.ObjectIdentifier{2, 5, 29, 27}
	// RFC5280, 5.2.5 id-ce-issuingDistributionPoint OBJECT IDENTIFIER ::= { id-ce 28 }
	oidIssuingDistributionPoint = asn1.ObjectIdentifier{2, 5, 29, 28}
	// RFC5280, 5.3.3 id-ce-certificateIssuer   OBJECT IDENTIFIER ::= { id-ce 29 }
	oidCertificateIssuer = asn1.ObjectIdentifier{2, 5, 29, 29}
	// RFC5290, 4.2.1.1 id-ce-authorityKeyIdentifier OBJECT IDENTIFIER ::=  { id-ce 35 }
	oidAuthorityKeyIdentifier = asn1.ObjectIdentifier{2, 5, 29, 35}
)

func checkCertRevocation(c *x509.Certificate, crl *CRL) (RevocationStatus, error) {
	// Per section 5.3.3 we prime the certificate issuer with the CRL issuer.
	// Subsequent entries use the previous entry's issuer.
	rawEntryIssuer := crl.RawIssuer

	// Loop through all the revoked certificates.
	for _, revCert := range crl.certList.RevokedCertificates {
		// 5.3 Loop through CRL entry extensions for needed information.
		for _, ext := range revCert.Extensions {
			if oidCertificateIssuer.Equal(ext.Id) {
				extIssuer, err := parseCertIssuerExt(ext)
				if err != nil {
					grpclogLogger.Info(err)
					if ext.Critical {
						return RevocationUndetermined, err
					}
					// Since this is a non-critical extension, we can skip it even though
					// there was a parsing failure.
					continue
				}
				rawEntryIssuer = extIssuer
			} else if ext.Critical {
				return RevocationUndetermined, fmt.Errorf("checkCertRevocation: Unhandled critical extension: %v", ext.Id)
			}
		}

		// If the issuer and serial number appear in the CRL, the certificate is revoked.
		if bytes.Equal(c.RawIssuer, rawEntryIssuer) && c.SerialNumber.Cmp(revCert.SerialNumber) == 0 {
			// CRL contains the serial, so return revoked.
			return RevocationRevoked, nil
		}
	}
	// We did not find the serial in the CRL file that was valid for the cert
	// so the certificate is not revoked.
	return RevocationUnrevoked, nil
}

func parseCertIssuerExt(ext pkix.Extension) ([]byte, error) {
	// 5.3.3 Certificate Issuer
	// CertificateIssuer ::=     GeneralNames
	// GeneralNames ::= SEQUENCE SIZE (1..MAX) OF GeneralName
	var generalNames []asn1.RawValue
	if rest, err := asn1.Unmarshal(ext.Value, &generalNames); err != nil || len(rest) != 0 {
		return nil, fmt.Errorf("asn1.Unmarshal failed: %v", err)
	}

	for _, generalName := range generalNames {
		// GeneralName ::= CHOICE {
		// otherName                       [0]     OtherName,
		// rfc822Name                      [1]     IA5String,
		// dNSName                         [2]     IA5String,
		// x400Address                     [3]     ORAddress,
		// directoryName                   [4]     Name,
		// ediPartyName                    [5]     EDIPartyName,
		// uniformResourceIdentifier       [6]     IA5String,
		// iPAddress                       [7]     OCTET STRING,
		// registeredID                    [8]     OBJECT IDENTIFIER }
		if generalName.Tag == tagDirectoryName {
			return generalName.Bytes, nil
		}
	}
	// Conforming CRL issuers MUST include in this extension the
	// distinguished name (DN) from the issuer field of the certificate that
	// corresponds to this CRL entry.
	// If we couldn't get a directoryName, we can't reason about this file so cert status is
	// RevocationUndetermined.
	return nil, errors.New("no DN found in certificate issuer")
}

// RFC 5280,  4.2.1.1
type authKeyID struct {
	ID []byte `asn1:"optional,tag:0"`
}

// RFC5280, 5.2.5
// id-ce-issuingDistributionPoint OBJECT IDENTIFIER ::= { id-ce 28 }

// IssuingDistributionPoint ::= SEQUENCE {
// 		distributionPoint          [0] DistributionPointName OPTIONAL,
// 		onlyContainsUserCerts      [1] BOOLEAN DEFAULT FALSE,
// 		onlyContainsCACerts        [2] BOOLEAN DEFAULT FALSE,
// 		onlySomeReasons            [3] ReasonFlags OPTIONAL,
// 		indirectCRL                [4] BOOLEAN DEFAULT FALSE,
// 		onlyContainsAttributeCerts [5] BOOLEAN DEFAULT FALSE }

// -- at most one of onlyContainsUserCerts, onlyContainsCACerts,
// -- and onlyContainsAttributeCerts may be set to TRUE.
type issuingDistributionPoint struct {
	DistributionPoint          asn1.RawValue  `asn1:"optional,tag:0"`
	OnlyContainsUserCerts      bool           `asn1:"optional,tag:1"`
	OnlyContainsCACerts        bool           `asn1:"optional,tag:2"`
	OnlySomeReasons            asn1.BitString `asn1:"optional,tag:3"`
	IndirectCRL                bool           `asn1:"optional,tag:4"`
	OnlyContainsAttributeCerts bool           `asn1:"optional,tag:5"`
}

// parseCRLExtensions parses the extensions for a CRL
// and checks that they're supported by the parser.
func parseCRLExtensions(c *x509.RevocationList) (*CRL, error) {
	if c == nil {
		return nil, errors.New("c is nil, expected any value")
	}
	certList := &CRL{certList: c}

	for _, ext := range c.Extensions {
		switch {
		case oidDeltaCRLIndicator.Equal(ext.Id):
			return nil, fmt.Errorf("delta CRLs unsupported")

		case oidAuthorityKeyIdentifier.Equal(ext.Id):
			var a authKeyID
			if rest, err := asn1.Unmarshal(ext.Value, &a); err != nil {
				return nil, fmt.Errorf("asn1.Unmarshal failed: %v", err)
			} else if len(rest) != 0 {
				return nil, errors.New("trailing data after AKID extension")
			}
			certList.authorityKeyID = a.ID

		case oidIssuingDistributionPoint.Equal(ext.Id):
			var dp issuingDistributionPoint
			if rest, err := asn1.Unmarshal(ext.Value, &dp); err != nil {
				return nil, fmt.Errorf("asn1.Unmarshal failed: %v", err)
			} else if len(rest) != 0 {
				return nil, errors.New("trailing data after IssuingDistributionPoint extension")
			}

			if dp.OnlyContainsUserCerts || dp.OnlyContainsCACerts || dp.OnlyContainsAttributeCerts {
				return nil, errors.New("CRL only contains some certificate types")
			}
			if dp.IndirectCRL {
				return nil, errors.New("indirect CRLs unsupported")
			}
			if dp.OnlySomeReasons.BitLength != 0 {
				return nil, errors.New("onlySomeReasons unsupported")
			}

		case ext.Critical:
			return nil, fmt.Errorf("unsupported critical extension: %v", ext.Id)
		}
	}

	if len(certList.authorityKeyID) == 0 {
		return nil, errors.New("authority key identifier extension missing")
	}
	return certList, nil
}

// pemType is the type of a PEM encoded CRL.
const pemType string = "X509 CRL"

var crlPemPrefix = []byte("-----BEGIN X509 CRL")

func crlPemToDer(crlBytes []byte) []byte {
	block, _ := pem.Decode(crlBytes)
	if block != nil && block.Type == pemType {
		crlBytes = block.Bytes
	}
	return crlBytes
}

// extractCRLIssuer extracts the raw ASN.1 encoding of the CRL issuer. Due to the design of
// pkix.CertificateList and pkix.RDNSequence, it is not possible to reliably marshal the
// parsed Issuer to it's original raw encoding.
func extractCRLIssuer(crlBytes []byte) ([]byte, error) {
	if bytes.HasPrefix(crlBytes, crlPemPrefix) {
		crlBytes = crlPemToDer(crlBytes)
	}
	der := cryptobyte.String(crlBytes)
	var issuer cryptobyte.String
	if !der.ReadASN1(&der, cbasn1.SEQUENCE) ||
		!der.ReadASN1(&der, cbasn1.SEQUENCE) ||
		!der.SkipOptionalASN1(cbasn1.INTEGER) ||
		!der.SkipASN1(cbasn1.SEQUENCE) ||
		!der.ReadASN1Element(&issuer, cbasn1.SEQUENCE) {
		return nil, errors.New("extractCRLIssuer: invalid ASN.1 encoding")
	}
	return issuer, nil
}

// parseRevocationList comes largely from here
// x509.go:
// https://github.com/golang/go/blob/e2f413402527505144beea443078649380e0c545/src/crypto/x509/x509.go#L1669-L1690
// We must first convert PEM to DER to be able to use the new
// x509.ParseRevocationList instead of the deprecated x509.ParseCRL
func parseRevocationList(crlBytes []byte) (*x509.RevocationList, error) {
	if bytes.HasPrefix(crlBytes, crlPemPrefix) {
		crlBytes = crlPemToDer(crlBytes)
	}
	crl, err := x509.ParseRevocationList(crlBytes)
	if err != nil {
		return nil, err
	}
	return crl, nil
}
