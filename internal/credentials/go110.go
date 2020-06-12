// +build go1.10

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
	"fmt"
	"net/url"

	"google.golang.org/grpc/grpclog"
)

// SPIFFEIDFromState parses the SPIFFE ID from State. An error is returned only
// when we are sure SPIFFE ID is used but the format is wrong.
func SPIFFEIDFromState(state tls.ConnectionState) (*url.URL, error) {
	if len(state.PeerCertificates) == 0 || len(state.PeerCertificates[0].URIs) == 0 {
		return nil, nil
	}
	spiffeIDCnt := 0
	var spiffeID url.URL
	for _, uri := range state.PeerCertificates[0].URIs {
		if uri == nil || uri.Scheme != "spiffe" || uri.Opaque != "" || (uri.User != nil && uri.User.Username() != "") {
			continue
		}
		// From this point, we assume the uri is intended for a SPIFFE ID.
		if len(uri.Host)+len(uri.Scheme)+len(uri.RawPath)+4 > 2048 ||
			len(uri.Host)+len(uri.Scheme)+len(uri.Path)+4 > 2048 {
			return nil, fmt.Errorf("invalid SPIFFE ID: total ID length larger than 2048 bytes")
		}
		if len(uri.Host) == 0 || len(uri.RawPath) == 0 || len(uri.Path) == 0 {
			return nil, fmt.Errorf("invalid SPIFFE ID: domain or workload ID is empty")
		}
		if len(uri.Host) > 255 {
			return nil, fmt.Errorf("invalid SPIFFE ID: domain length larger than 255 characters")
		}
		// We use a default deep copy since we know the User field of a SPIFFE ID
		// is empty.
		spiffeID = *uri
		spiffeIDCnt++
	}
	if spiffeIDCnt == 1 {
		return &spiffeID, nil
	} else if spiffeIDCnt > 1 {
		// A standard SPIFFE ID should be unique. If there are more than one ID, we
		// should log this error but shouldn't halt the application.
		grpclog.Warning("invalid SPIFFE ID: multiple SPIFFE IDs")
		return nil, nil
	}
	// SPIFFE ID is not used.
	return nil, nil
}
