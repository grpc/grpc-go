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
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/cryptobyte"
	cbasn1 "golang.org/x/crypto/cryptobyte/asn1"
	"google.golang.org/grpc/grpclog"
)

var grpclogLogger = grpclog.Component("advancedtls")

// Cache is an interface to cache CRL files.
// The cache implementation must be concurrency safe.
// A fixed size lru cache from golang-lru is recommended.
type Cache interface {
	// Add adds a value to the cache.
	Add(key, value interface{}) bool
	// Get looks up a key's value from the cache.
	Get(key interface{}) (value interface{}, ok bool)
}

// RevocationConfig contains options for CRL lookup.
type RevocationConfig struct {
	// RootDir is the directory to search for CRL files.
	// Directory format must match OpenSSL X509_LOOKUP_hash_dir(3).
	RootDir string
	// AllowUndetermined controls if certificate chains with RevocationUndetermined
	// revocation status are allowed to complete.
	AllowUndetermined bool
	// Cache will store CRL files if not nil, otherwise files are reloaded for every lookup.
	Cache Cache
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

// certificateListExt contains a pkix.CertificateList and parsed
// extensions that aren't provided by the golang CRL parser.
type certificateListExt struct {
	CertList *pkix.CertificateList
	// RFC5280, 5.2.1, all conforming CRLs must have a AKID with the ID method.
	AuthorityKeyID []byte
	RawIssuer      []byte
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

// x509NameHash implements the OpenSSL X509_NAME_hash function for hashed directory lookups.
//
// NOTE: due to the behavior of asn1.Marshal, if the original encoding of the RDN sequence
// contains strings which do not use the ASN.1 PrintableString type, the name will not be
// re-encoded using those types, resulting in a hash which does not match that produced
// by OpenSSL.
func x509NameHash(r pkix.RDNSequence) string {
	var canonBytes []byte
	// First, canonicalize all the strings.
	for _, rdnSet := range r {
		for i, rdn := range rdnSet {
			value, ok := rdn.Value.(string)
			if !ok {
				continue
			}
			// OpenSSL trims all whitespace, does a tolower, and removes extra spaces between words.
			// Implemented in x509_name_canon in OpenSSL
			canonStr := strings.Join(strings.Fields(
				strings.TrimSpace(strings.ToLower(value))), " ")
			// Then it changes everything to UTF8 strings
			rdnSet[i].Value = asn1.RawValue{Tag: asn1.TagUTF8String, Bytes: []byte(canonStr)}

		}
	}

	// Finally, OpenSSL drops the initial sequence tag
	// so we marshal all the RDNs separately instead of as a group.
	for _, canonRdn := range r {
		b, err := asn1.Marshal(canonRdn)
		if err != nil {
			continue
		}
		canonBytes = append(canonBytes, b...)
	}

	issuerHash := sha1.Sum(canonBytes)
	// Openssl takes the first 4 bytes and encodes them as a little endian
	// uint32 and then uses the hex to make the file name.
	// In C++, this would be:
	// (((unsigned long)md[0]) | ((unsigned long)md[1] << 8L) |
	// ((unsigned long)md[2] << 16L) | ((unsigned long)md[3] << 24L)
	// ) & 0xffffffffL;
	fileHash := binary.LittleEndian.Uint32(issuerHash[0:4])
	return fmt.Sprintf("%08x", fileHash)
}

// CheckRevocation checks the connection for revoked certificates based on RFC5280.
// This implementation has the following major limitations:
//   - Indirect CRL files are not supported.
//   - CRL loading is only supported from directories in the X509_LOOKUP_hash_dir format.
//   - OnlySomeReasons is not supported.
//   - Delta CRL files are not supported.
//   - Certificate CRLDistributionPoint must be URLs, but are then ignored and converted into a file path.
//   - CRL checks are done after path building, which goes against RFC4158.
func CheckRevocation(conn tls.ConnectionState, cfg RevocationConfig) error {
	return CheckChainRevocation(conn.VerifiedChains, cfg)
}

// CheckChainRevocation checks the verified certificate chain
// for revoked certificates based on RFC5280.
func CheckChainRevocation(verifiedChains [][]*x509.Certificate, cfg RevocationConfig) error {
	// Iterate the verified chains looking for one that is RevocationUnrevoked.
	// A single RevocationUnrevoked chain is enough to allow the connection, and a single RevocationRevoked
	// chain does not mean the connection should fail.
	count := make(map[RevocationStatus]int)
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
			if cfg.AllowUndetermined {
				return nil
			}
			count[RevocationUndetermined]++
			continue
		}
	}
	return fmt.Errorf("no unrevoked chains found: %v", count)
}

// checkChain will determine and check all certificates in chain against the CRL
// defined in the certificate with the following rules:
// 1. If any certificate is RevocationRevoked, return RevocationRevoked.
// 2. If any certificate is RevocationUndetermined, return RevocationUndetermined.
// 3. If all certificates are RevocationUnrevoked, return RevocationUnrevoked.
func checkChain(chain []*x509.Certificate, cfg RevocationConfig) RevocationStatus {
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

func cachedCrl(rawIssuer []byte, cache Cache) (*certificateListExt, bool) {
	val, ok := cache.Get(hex.EncodeToString(rawIssuer))
	if !ok {
		return nil, false
	}
	crl, ok := val.(*certificateListExt)
	if !ok {
		return nil, false
	}
	// If the CRL is expired, force a reload.
	if crl.CertList.HasExpired(time.Now()) {
		return nil, false
	}
	return crl, true
}

// fetchIssuerCRL fetches and verifies the CRL for rawIssuer from disk or cache if configured in cfg.
func fetchIssuerCRL(rawIssuer []byte, crlVerifyCrt []*x509.Certificate, cfg RevocationConfig) (*certificateListExt, error) {
	if cfg.Cache != nil {
		if crl, ok := cachedCrl(rawIssuer, cfg.Cache); ok {
			return crl, nil
		}
	}

	crl, err := fetchCRL(rawIssuer, cfg)
	if err != nil {
		return nil, fmt.Errorf("fetchCRL() failed err = %v", err)
	}

	if err := verifyCRL(crl, rawIssuer, crlVerifyCrt); err != nil {
		return nil, fmt.Errorf("verifyCRL() failed err = %v", err)
	}
	if cfg.Cache != nil {
		cfg.Cache.Add(hex.EncodeToString(rawIssuer), crl)
	}
	return crl, nil
}

// checkCert checks a single certificate against the CRL defined in the certificate.
// It will fetch and verify the CRL(s) defined in the root directory specified by cfg.
// If we can't load any authoritative CRL files, the status is RevocationUndetermined.
// c is the certificate to check.
// crlVerifyCrt is the group of possible certificates to verify the crl.
func checkCert(c *x509.Certificate, crlVerifyCrt []*x509.Certificate, cfg RevocationConfig) RevocationStatus {
	crl, err := fetchIssuerCRL(c.RawIssuer, crlVerifyCrt, cfg)
	if err != nil {
		// We couldn't load any CRL files for the certificate, so we don't know if it's RevocationUnrevoked or not.
		grpclogLogger.Warningf("getIssuerCRL(%v) err = %v", c.Issuer, err)
		return RevocationUndetermined
	}
	revocation, err := checkCertRevocation(c, crl)
	if err != nil {
		grpclogLogger.Warningf("checkCertRevocation(CRL %v) failed %v", crl.CertList.TBSCertList.Issuer, err)
		// We couldn't check the CRL file for some reason, so we don't know if it's RevocationUnrevoked or not.
		return RevocationUndetermined
	}
	// Here we've gotten a CRL that loads and verifies.
	// We only handle all-reasons CRL files, so this file
	// is authoritative for the certificate.
	return revocation
}

func checkCertRevocation(c *x509.Certificate, crl *certificateListExt) (RevocationStatus, error) {
	// Per section 5.3.3 we prime the certificate issuer with the CRL issuer.
	// Subsequent entries use the previous entry's issuer.
	rawEntryIssuer := crl.RawIssuer

	// Loop through all the revoked certificates.
	for _, revCert := range crl.CertList.TBSCertList.RevokedCertificates {
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
		return nil, fmt.Errorf("asn1.Unmarshal failed err = %v", err)
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
func parseCRLExtensions(c *pkix.CertificateList) (*certificateListExt, error) {
	if c == nil {
		return nil, errors.New("c is nil, expected any value")
	}
	certList := &certificateListExt{CertList: c}

	for _, ext := range c.TBSCertList.Extensions {
		switch {
		case oidDeltaCRLIndicator.Equal(ext.Id):
			return nil, fmt.Errorf("delta CRLs unsupported")

		case oidAuthorityKeyIdentifier.Equal(ext.Id):
			var a authKeyID
			if rest, err := asn1.Unmarshal(ext.Value, &a); err != nil {
				return nil, fmt.Errorf("asn1.Unmarshal failed. err = %v", err)
			} else if len(rest) != 0 {
				return nil, errors.New("trailing data after AKID extension")
			}
			certList.AuthorityKeyID = a.ID

		case oidIssuingDistributionPoint.Equal(ext.Id):
			var dp issuingDistributionPoint
			if rest, err := asn1.Unmarshal(ext.Value, &dp); err != nil {
				return nil, fmt.Errorf("asn1.Unmarshal failed. err = %v", err)
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

	if len(certList.AuthorityKeyID) == 0 {
		return nil, errors.New("authority key identifier extension missing")
	}
	return certList, nil
}

func fetchCRL(rawIssuer []byte, cfg RevocationConfig) (*certificateListExt, error) {
	var parsedCRL *certificateListExt
	// 6.3.3 (a) (1) (ii)
	// According to X509_LOOKUP_hash_dir the format is issuer_hash.rN where N is an increasing number.
	// There are no gaps, so we break when we can't find a file.
	for i := 0; ; i++ {
		// Unmarshal to RDNSeqence according to http://go/godoc/crypto/x509/pkix/#Name.
		var r pkix.RDNSequence
		rest, err := asn1.Unmarshal(rawIssuer, &r)
		if len(rest) != 0 || err != nil {
			return nil, fmt.Errorf("asn1.Unmarshal(Issuer) len(rest) = %v, err = %v", len(rest), err)
		}
		crlPath := fmt.Sprintf("%s.r%d", filepath.Join(cfg.RootDir, x509NameHash(r)), i)
		crlBytes, err := ioutil.ReadFile(crlPath)
		if err != nil {
			// Break when we can't read a CRL file.
			grpclogLogger.Infof("readFile: %v", err)
			break
		}

		crl, err := x509.ParseCRL(crlBytes)
		if err != nil {
			// Parsing errors for a CRL shouldn't happen so fail.
			return nil, fmt.Errorf("x509.ParseCrl(%v) failed err = %v", crlPath, err)
		}
		var certList *certificateListExt
		if certList, err = parseCRLExtensions(crl); err != nil {
			grpclogLogger.Infof("fetchCRL: unsupported crl %v, err = %v", crlPath, err)
			// Continue to find a supported CRL
			continue
		}

		rawCRLIssuer, err := extractCRLIssuer(crlBytes)
		if err != nil {
			return nil, err
		}
		certList.RawIssuer = rawCRLIssuer
		// RFC5280, 6.3.3 (b) Verify the issuer and scope of the complete CRL.
		if bytes.Equal(rawIssuer, rawCRLIssuer) {
			parsedCRL = certList
			// Continue to find the highest number in the .rN suffix.
			continue
		}
	}

	if parsedCRL == nil {
		return nil, fmt.Errorf("fetchCrls no CRLs found for issuer")
	}
	return parsedCRL, nil
}

func verifyCRL(crl *certificateListExt, rawIssuer []byte, chain []*x509.Certificate) error {
	// RFC5280, 6.3.3 (f) Obtain and validateate the certification path for the issuer of the complete CRL
	// We intentionally limit our CRLs to be signed with the same certificate path as the certificate
	// so we can use the chain from the connection.

	for _, c := range chain {
		// Use the key where the subject and KIDs match.
		// This departs from RFC4158, 3.5.12 which states that KIDs
		// cannot eliminate certificates, but RFC5280, 5.2.1 states that
		// "Conforming CRL issuers MUST use the key identifier method, and MUST
		// include this extension in all CRLs issued."
		// So, this is much simpler than RFC4158 and should be compatible.
		if bytes.Equal(c.SubjectKeyId, crl.AuthorityKeyID) && bytes.Equal(c.RawSubject, crl.RawIssuer) {
			// RFC5280, 6.3.3 (g) Validate signature.
			return c.CheckCRLSignature(crl.CertList)
		}
	}
	return fmt.Errorf("verifyCRL: No certificates mached CRL issuer (%v)", crl.CertList.TBSCertList.Issuer)
}

var crlPemPrefix = []byte("-----BEGIN X509 CRL")

// extractCRLIssuer extracts the raw ASN.1 encoding of the CRL issuer. Due to the design of
// pkix.CertificateList and pkix.RDNSequence, it is not possible to reliably marshal the
// parsed Issuer to it's original raw encoding.
func extractCRLIssuer(crlBytes []byte) ([]byte, error) {
	if bytes.HasPrefix(crlBytes, crlPemPrefix) {
		block, _ := pem.Decode(crlBytes)
		if block != nil && block.Type == "X509 CRL" {
			crlBytes = block.Bytes
		}
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
