/*
 *
 * Copyright 2025 gRPC authors.
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

// Package spiffe defines APIs for working with SPIFFE Bundle Maps.
//
// All APIs in this package are experimental.
package spiffe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spiffe/go-spiffe/v2/bundle/spiffebundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// SPIFFEBundleMap represents a SPIFFE Bundle Map per the spec
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format.
type SPIFFEBundleMap map[string]*spiffebundle.Bundle

type partialParsedSPIFFEBundleMap struct {
	Bundles map[string]json.RawMessage `json:"trust_domains"`
}

// LoadSPIFFEBundleMap loads a SPIFFE Bundle Map from a file. See the SPIFFE
// Bundle Map spec for more detail -
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format
// If duplicate keys are encountered in the JSON parsing, Go's default unmarshal
// behavior occurs which causes the last processed entry to be the entry in the
// parsed map.
func LoadSPIFFEBundleMap(filePath string) (SPIFFEBundleMap, error) {
	bundleMapRaw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return SPIFFEBundleMapFromBytes(bundleMapRaw)
}

// SPIFFEBundleMapFromBytes parses bytes into a SPIFFE Bundle Map. See the
// SPIFFE Bundle Map spec for more detail -
// https://github.com/spiffe/spiffe/blob/main/standards/SPIFFE_Trust_Domain_and_Bundle.md#4-spiffe-bundle-format
// If duplicate keys are encountered in the JSON parsing, Go's default unmarshal
// behavior occurs which causes the last processed entry to be the entry in the
// parsed map.
func SPIFFEBundleMapFromBytes(bundleMapBytes []byte) (SPIFFEBundleMap, error) {
	var result partialParsedSPIFFEBundleMap
	err := json.Unmarshal(bundleMapBytes, &result)
	if err != nil {
		return nil, err
	}
	if result.Bundles == nil {
		return nil, errors.New("no content in spiffe bundle map file")
	}
	bundleMap := map[string]*spiffebundle.Bundle{}
	for trustDomainString, jsonBundle := range result.Bundles {
		trustDomain, err := spiffeid.TrustDomainFromString(trustDomainString)
		if err != nil {
			return nil, fmt.Errorf("invalud trust domain found when parsing map: %v", err)
		}
		bundle, err := spiffebundle.Parse(trustDomain, jsonBundle)
		if err != nil {
<<<<<<< HEAD
			return nil, fmt.Errorf("failed to parse bundle in map: %v", err)
=======
			return nil, fmt.Errorf("spiffe: failed to parse bundle in map file (%v) for trust domain %v: %v", filePath, td, err)
>>>>>>> spiffe
		}
		bundleMap[trustDomainString] = bundle
	}
	return bundleMap, nil

}
