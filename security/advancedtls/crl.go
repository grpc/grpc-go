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
	"google.golang.org/grpc/grpclog"
)

var grpclogLogger = grpclog.Component("advancedtls")

// RevocationOptions allows a user to configure certificate revocation behavior.
type RevocationOptions struct {
	// DenyUndetermined controls if certificate chains with RevocationUndetermined
	// revocation status are allowed to complete.
	DenyUndetermined bool
	// CRLProvider is an alternative to using RootDir directly for the
	// X509_LOOKUP_hash_dir approach to CRL files. If set, the CRLProvider's CRL
	// function will be called when looking up and fetching CRLs during the
	// handshake.
	CRLProvider CRLProvider
}

// revocationStatus is the revocation status for a certificate or chain.
type revocationStatus int

const (
	// RevocationUndetermined means we couldn't find or verify a CRL for the cert.
	RevocationUndetermined revocationStatus = iota
	// RevocationUnrevoked means we found the CRL for the cert and the cert is not revoked.
	RevocationUnrevoked
	// RevocationRevoked means we found the CRL and the cert is revoked.
	RevocationRevoked
)

// CRL contains a pkix.CertificateList and parsed extensions that aren't
// provided by the golang CRL parser.
// All CRLs should be loaded using NewCRL() for bytes directly or ReadCRLFile()
// to read directly from a filepath
type CRL struct {
	certList *x509.RevocationList
	// RFC5280, 5.2.1, all conforming CRLs must have a AKID with the ID method.
	authorityKeyID []byte
	rawIssuer      []byte
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
	crlExt.rawIssuer, err = extractCRLIssuer(b)
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

// checkChainRevocation checks the verified certificate chain
// for revoked certificates based on RFC5280.
func checkChainRevocation(verifiedChains [][]*x509.Certificate, cfg RevocationOptions) error {
	// Iterate the verified chains looking for one that is RevocationUnrevoked.
	// A single RevocationUnrevoked chain is enough to allow the connection, and a single RevocationRevoked
	// chain does not mean the connection should fail.
	count := make(map[revocationStatus]int)
	for _, chain := range verifiedChains {
		switch checkChain(chain, cfg) {
		case RevocationUnrevoked:
			// If any chain is RevocationUnrevoked then return no error.
			return nil
		case RevocationRevoked:
			// If this chain is revoked, keep looking for another chain.
			count[RevocationRevoked]++
			continue
		case RevocationUndetermined:
			count[RevocationUndetermined]++
			if cfg.DenyUndetermined {
				continue
			}
			return nil
		}
	}
	return fmt.Errorf("no unrevoked chains found: %v", count)
}

// checkChain will determine and check all certificates in chain against the CRL
// defined in the certificate with the following rules:
// 1. If any certificate is RevocationRevoked, return RevocationRevoked.
// 2. If any certificate is RevocationUndetermined, return RevocationUndetermined.
// 3. If all certificates are RevocationUnrevoked, return RevocationUnrevoked.
func checkChain(chain []*x509.Certificate, cfg RevocationOptions) revocationStatus {
	chainStatus := RevocationUnrevoked
	for _, c := range chain {
		switch checkCert(c, chain, cfg) {
		case RevocationRevoked:
			// Easy case, if a cert in the chain is revoked, the chain is revoked.
			return RevocationRevoked
		case RevocationUndetermined:
			// If we couldn't find the revocation status for a cert, the chain is at best RevocationUndetermined
			// keep looking to see if we find a cert in the chain that's RevocationRevoked,
			// but return RevocationUndetermined at a minimum.
			chainStatus = RevocationUndetermined
		case RevocationUnrevoked:
			// Continue iterating up the cert chain.
			continue
		}
	}
	return chainStatus
}

func fetchCRL(c *x509.Certificate, crlVerifyCrt []*x509.Certificate, cfg RevocationOptions) (*CRL, error) {
	if cfg.CRLProvider == nil {
		return nil, fmt.Errorf("trying to fetch CRL but CRLProvider is nil")
	}
	crl, err := cfg.CRLProvider.CRL(c)
	if err != nil {
		return nil, fmt.Errorf("CrlProvider failed err = %v", err)
	}
	if crl == nil {
		return nil, fmt.Errorf("no CRL found for certificate's issuer")
	}
	if err := verifyCRL(crl, crlVerifyCrt); err != nil {
		return nil, fmt.Errorf("verifyCRL() failed: %v", err)
	}
	return crl, nil
}

// checkCert checks a single certificate against the CRL defined in the
// certificate. It will fetch and verify the CRL(s) defined in the root
// directory (or a CRLProvider) specified by cfg. If we can't load (and verify -
// see verifyCRL) any valid authoritative CRL files, the status is
// RevocationUndetermined.
// c is the certificate to check.
// crlVerifyCrt is the group of possible certificates to verify the crl.
func checkCert(c *x509.Certificate, crlVerifyCrt []*x509.Certificate, cfg RevocationOptions) revocationStatus {
	crl, err := fetchCRL(c, crlVerifyCrt, cfg)
	if err != nil {
		// We couldn't load any valid CRL files for the certificate, so we don't
		// know if it's RevocationUnrevoked or not. This is not necessarily a
		// problem - it's not invalid to have no CRLs if you don't have any
		// revocations for an issuer. It also might be an indication that the CRL
		// file is invalid.
		// We just return RevocationUndetermined and there is a setting for the user
		// to control the handling of that.
		grpclogLogger.Warningf("fetchCRL() err = %v", err)
		return RevocationUndetermined
	}
	revocation, err := checkCertRevocation(c, crl)
	if err != nil {
		grpclogLogger.Warningf("checkCertRevocation(CRL %v) failed: %v", crl.certList.Issuer, err)
		// We couldn't check the CRL file for some reason, so we don't know if it's RevocationUnrevoked or not.
		return RevocationUndetermined
	}
	// Here we've gotten a CRL that loads and verifies.
	// We only handle all-reasons CRL files, so this file
	// is authoritative for the certificate.
	return revocation
}

func checkCertRevocation(c *x509.Certificate, crl *CRL) (revocationStatus, error) {
	// Per section 5.3.3 we prime the certificate issuer with the CRL issuer.
	// Subsequent entries use the previous entry's issuer.
	rawEntryIssuer := crl.rawIssuer

	// Loop through all the revoked certificates.
	for _, revCert := range crl.certList.RevokedCertificateEntries {
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

func verifyCRL(crl *CRL, chain []*x509.Certificate) error {
	// RFC5280, 6.3.3 (f) Obtain and validate the certification path for the issuer of the complete CRL
	// We intentionally limit our CRLs to be signed with the same certificate path as the certificate
	// so we can use the chain from the connection.

	for _, c := range chain {
		// Use the key where the subject and KIDs match.
		// This departs from RFC4158, 3.5.12 which states that KIDs
		// cannot eliminate certificates, but RFC5280, 5.2.1 states that
		// "Conforming CRL issuers MUST use the key identifier method, and MUST
		// include this extension in all CRLs issued."
		// So, this is much simpler than RFC4158 and should be compatible.
		if bytes.Equal(c.SubjectKeyId, crl.authorityKeyID) && bytes.Equal(c.RawSubject, crl.rawIssuer) {
			// RFC5280, 6.3.3 (f) Key usage and cRLSign bit.
			if c.KeyUsage != 0 && c.KeyUsage&x509.KeyUsageCRLSign == 0 {
				return fmt.Errorf("verifyCRL: The certificate can't be used for issuing CRLs")
			}
			// RFC5280, 6.3.3 (g) Validate signature.
			return crl.certList.CheckSignatureFrom(c)
		}
	}
	return fmt.Errorf("verifyCRL: No certificates matched CRL issuer (%v)", crl.certList.Issuer)
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
// parsed Issuer to its original raw encoding.
func extractCRLIssuer(crlBytes []byte) ([]byte, error) {
	if bytes.HasPrefix(crlBytes, crlPemPrefix) {
		crlBytes = crlPemToDer(crlBytes)
	}
	der := cryptobyte.String(crlBytes)
	var issuer cryptobyte.String
	// This doubled der.ReadASN1 is intentional, it modifies the input buffer
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
