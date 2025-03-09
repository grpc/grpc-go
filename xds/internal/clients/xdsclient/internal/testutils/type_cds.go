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

package testutils

import (
	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/testutils/matcher"
)

// SecurityConfig contains the security configuration received as part of the
// Cluster resource on the client-side, and as part of the Listener resource on
// the server-side.
type SecurityConfig struct {
	// RootInstanceName identifies the certProvider plugin to be used to fetch
	// root certificates. This instance name will be resolved to the plugin name
	// and its associated configuration from the certificate_providers field of
	// the bootstrap file.
	RootInstanceName string
	// RootCertName is the certificate name to be passed to the plugin (looked
	// up from the bootstrap file) while fetching root certificates.
	RootCertName string
	// IdentityInstanceName identifies the certProvider plugin to be used to
	// fetch identity certificates. This instance name will be resolved to the
	// plugin name and its associated configuration from the
	// certificate_providers field of the bootstrap file.
	IdentityInstanceName string
	// IdentityCertName is the certificate name to be passed to the plugin
	// (looked up from the bootstrap file) while fetching identity certificates.
	IdentityCertName string
	// SubjectAltNameMatchers is an optional list of match criteria for SANs
	// specified on the peer certificate. Used only on the client-side.
	//
	// Some intricacies:
	// - If this field is empty, then any peer certificate is accepted.
	// - If the peer certificate contains a wildcard DNS SAN, and an `exact`
	//   matcher is configured, a wildcard DNS match is performed instead of a
	//   regular string comparison.
	SubjectAltNameMatchers []matcher.StringMatcher
	// RequireClientCert indicates if the server handshake process expects the
	// client to present a certificate. Set to true when performing mTLS. Used
	// only on the server-side.
	RequireClientCert bool
	// UseSystemRootCerts indicates that the client should use system root
	// certificates to validate the server certificate. This field is mutually
	// exclusive with RootCertName and RootInstanceName. Validation performed
	// after unmarshalling xDS resources ensures that this field is set only
	// when both RootCertName and RootInstanceName are empty.
	UseSystemRootCerts bool
}

// Equal returns true if sc is equal to other.
func (sc *SecurityConfig) Equal(other *SecurityConfig) bool {
	switch {
	case sc == nil && other == nil:
		return true
	case (sc != nil) != (other != nil):
		return false
	}
	switch {
	case sc.RootInstanceName != other.RootInstanceName:
		return false
	case sc.RootCertName != other.RootCertName:
		return false
	case sc.IdentityInstanceName != other.IdentityInstanceName:
		return false
	case sc.IdentityCertName != other.IdentityCertName:
		return false
	case sc.RequireClientCert != other.RequireClientCert:
		return false
	case sc.UseSystemRootCerts != other.UseSystemRootCerts:
		return false
	default:
		if len(sc.SubjectAltNameMatchers) != len(other.SubjectAltNameMatchers) {
			return false
		}
		for i := 0; i < len(sc.SubjectAltNameMatchers); i++ {
			if !sc.SubjectAltNameMatchers[i].Equal(other.SubjectAltNameMatchers[i]) {
				return false
			}
		}
	}
	return true
}
