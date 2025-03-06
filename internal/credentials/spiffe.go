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

// Package credentials defines APIs for parsing SPIFFE ID.
//
// All APIs in this package are experimental.
package credentials

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc/grpclog"
)

var logger = grpclog.Component("credentials")

// SPIFFEIDFromState parses the SPIFFE ID from State. If the SPIFFE ID format
// is invalid, return nil with warning.
func SPIFFEIDFromState(state tls.ConnectionState) *url.URL {
	if len(state.PeerCertificates) == 0 || len(state.PeerCertificates[0].URIs) == 0 {
		return nil
	}
	return SPIFFEIDFromCert(state.PeerCertificates[0])
}

// SPIFFEIDFromCert parses the SPIFFE ID from x509.Certificate. If the SPIFFE
// ID format is invalid, return nil with warning.
func SPIFFEIDFromCert(cert *x509.Certificate) *url.URL {
	if cert == nil || cert.URIs == nil {
		return nil
	}
	var spiffeID *url.URL
	for _, uri := range cert.URIs {
		if uri == nil || uri.Scheme != "spiffe" || uri.Opaque != "" || (uri.User != nil && uri.User.Username() != "") {
			continue
		}
		// From this point, we assume the uri is intended for a SPIFFE ID.
		if len(uri.String()) > 2048 {
			logger.Warning("invalid SPIFFE ID: total ID length larger than 2048 bytes")
			return nil
		}
		if len(uri.Host) == 0 || len(uri.Path) == 0 {
			logger.Warning("invalid SPIFFE ID: domain or workload ID is empty")
			return nil
		}
		if len(uri.Host) > 255 {
			logger.Warning("invalid SPIFFE ID: domain length larger than 255 characters")
			return nil
		}
		// A valid SPIFFE certificate can only have exactly one URI SAN field.
		if len(cert.URIs) > 1 {
			logger.Warning("invalid SPIFFE ID: multiple URI SANs")
			return nil
		}
		spiffeID = uri
	}
	return spiffeID
}

type partialParsedSPIFFEBundleMap struct {
	Bundles map[string]json.RawMessage `json:"trust_domains"`
}

// LoadSPIFFEBundleMap loads a SPIFFE Bundle Map from a file. See the SPIFFE
// Bundle Map spec for more detail -
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format
// If duplicate keys are encountered in the JSON parsing, Go's default unmarshal
// behavior occurs which causes the last processed entry to be the entry in the
// parsed map.
func LoadSPIFFEBundleMap(filePath string) (map[string]*spiffebundle.Bundle, error) {
	bundleMapRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var result partialParsedSPIFFEBundleMap
	if err := json.Unmarshal(bundleMapRaw, &result); err != nil {
		return nil, err
	}
	if result.Bundles == nil {
		return nil, fmt.Errorf("spiffe: no content in spiffe bundle map file %v", filePath)
	}
	bundleMap := map[string]*spiffebundle.Bundle{}
	for td, jsonBundle := range result.Bundles {
		trustDomain, err := spiffeid.TrustDomainFromString(td)
		if err != nil {
			return nil, fmt.Errorf("spiffe: invalid trust domain (%v) found when parsing map file (%v): %v", td, filePath, err)
		}
		bundle, err := spiffebundle.Parse(trustDomain, jsonBundle)
		if err != nil {
			return nil, fmt.Errorf("spiffe: failed to parse bundle in map file (%v): %v", filePath, err)
		}
		bundleMap[td] = bundle
	}
	return bundleMap, nil
}
