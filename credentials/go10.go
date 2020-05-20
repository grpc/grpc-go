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

package credentials

import (
	"fmt"
	"net/url"

	"google.golang.org/grpc/grpclog"
)

// ParseSpiffeID parses the Spiffe ID from State and fill it into SpiffeID.
// An error is returned only when we are sure Spiffe ID is used but the format
// is wrong.
// This function can only be used with go version 1.10 and onwards. When used
// with a prior version, no error will be returned, but the field
// TLSInfo.SpiffeID wouldn't be plumbed.
func (t *TLSInfo) ParseSpiffeID() error {
	if len(t.State.PeerCertificates) == 0 || len(t.State.PeerCertificates[0].URIs) == 0 {
		return nil
	}
	spiffeIDCnt := 0
	var spiffeID url.URL
	for _, uri := range t.State.PeerCertificates[0].URIs {
		if uri == nil || uri.Scheme != "spiffe" || uri.Opaque != "" || (uri.User != nil && uri.User.Username() != "") {
			continue
		}
		// From this point, we assume the uri is intended for a Spiffe ID.
		if len(uri.Host)+len(uri.Scheme)+len(uri.RawPath)+4 > 2048 ||
			len(uri.Host)+len(uri.Scheme)+len(uri.Path)+4 > 2048 {
			return fmt.Errorf("invalid SPIFFE ID: total ID length larger than 2048 bytes")
		}
		if len(uri.Host) == 0 || len(uri.RawPath) == 0 || len(uri.Path) == 0 {
			return fmt.Errorf("invalid SPIFFE ID: domain or workload ID is empty")
		}
		if len(uri.Host) > 255 {
			return fmt.Errorf("invalid SPIFFE ID: domain length larger than 255 characters")
		}
		// We use a default deep copy since we know the User field of a SPIFFE ID is empty.
		spiffeID = *uri
		spiffeIDCnt++
	}
	if spiffeIDCnt == 1 {
		t.SpiffeID = &spiffeID
	} else {
		// A standard SPIFFE ID should be unique. If there are more, we log this
		// mis-behavior and not plumb any of them.
		grpclog.Info("invalid SPIFFE ID: multiple SPIFFE IDs")
	}
	return nil
}
