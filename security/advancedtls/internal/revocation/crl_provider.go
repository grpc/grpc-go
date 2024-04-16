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

package revocation

import (
	"crypto/x509"
)

// CRLProvider is the interface to be implemented to enable custom CRL provider
// behavior, as defined in [gRFC A69].
//
// The interface defines how gRPC gets CRLs from the provider during handshakes,
// but doesn't prescribe a specific way to load and store CRLs. Such
// implementations can be used in RevocationConfig of advancedtls.ClientOptions
// and/or advancedtls.ServerOptions.
// Please note that checking CRLs is directly on the path of connection
// establishment, so implementations of the CRL function need to be fast, and
// slow things such as file IO should be done asynchronously.
//
// [gRFC A69]: https://github.com/grpc/proposal/pull/382
type CRLProvider interface {
	// CRL accepts x509 Cert and returns a related CRL struct, which can contain
	// either an empty or non-empty list of revoked certificates. If an error is
	// thrown or (nil, nil) is returned, it indicates that we can't load any
	// authoritative CRL files (which may not necessarily be a problem). It's not
	// considered invalid to have no CRLs if there are no revocations for an
	// issuer. In such cases, the status of the check CRL operation is marked as
	// RevocationUndetermined, as defined in [RFC5280 - Undetermined].
	//
	// [RFC5280 - Undetermined]: https://datatracker.ietf.org/doc/html/rfc5280#section-6.3.3
	CRL(cert *x509.Certificate) (*CRL, error)
}
