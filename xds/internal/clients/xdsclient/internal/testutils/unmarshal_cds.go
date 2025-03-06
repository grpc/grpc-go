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
	"errors"
	"fmt"

	"google.golang.org/grpc/internal/envconfig"

	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/testutils/matcher"

	v3tlspb "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
)

// TransportSocket proto message has a `name` field which is expected to be set
// to this value by the management server.
const transportSocketName = "envoy.transport_sockets.tls"

// common is expected to be not nil.
// The `alpn_protocols` field is ignored.
func securityConfigFromCommonTLSContext(common *v3tlspb.CommonTlsContext, server bool) (*SecurityConfig, error) {
	if common.GetTlsParams() != nil {
		return nil, fmt.Errorf("unsupported tls_params field in CommonTlsContext message: %+v", common)
	}
	if common.GetCustomHandshaker() != nil {
		return nil, fmt.Errorf("unsupported custom_handshaker field in CommonTlsContext message: %+v", common)
	}

	// For now, if we can't get a valid security config from the new fields, we
	// fallback to the old deprecated fields.
	// TODO: Drop support for deprecated fields. NACK if err != nil here.
	sc, err1 := securityConfigFromCommonTLSContextUsingNewFields(common, server)
	if sc == nil || sc.Equal(&SecurityConfig{}) {
		var err error
		sc, err = securityConfigFromCommonTLSContextWithDeprecatedFields(common, server)
		if err != nil {
			// Retain the validation error from using the new fields.
			return nil, errors.Join(err1, fmt.Errorf("failed to parse config using deprecated fields: %v", err))
		}
	}
	if sc != nil {
		// sc == nil is a valid case where the control plane has not sent us any
		// security configuration. xDS creds will use fallback creds.
		if server {
			if sc.IdentityInstanceName == "" {
				return nil, errors.New("security configuration on the server-side does not contain identity certificate provider instance name")
			}
		} else {
			if !sc.UseSystemRootCerts && sc.RootInstanceName == "" {
				return nil, errors.New("security configuration on the client-side does not contain root certificate provider instance name")
			}
		}
	}
	return sc, nil
}

func securityConfigFromCommonTLSContextWithDeprecatedFields(common *v3tlspb.CommonTlsContext, server bool) (*SecurityConfig, error) {
	// The `CommonTlsContext` contains a
	// `tls_certificate_certificate_provider_instance` field of type
	// `CertificateProviderInstance`, which contains the provider instance name
	// and the certificate name to fetch identity certs.
	sc := &SecurityConfig{}
	if identity := common.GetTlsCertificateCertificateProviderInstance(); identity != nil {
		sc.IdentityInstanceName = identity.GetInstanceName()
		sc.IdentityCertName = identity.GetCertificateName()
	}

	// The `CommonTlsContext` contains a `validation_context_type` field which
	// is a oneof. We can get the values that we are interested in from two of
	// those possible values:
	//  - combined validation context:
	//    - contains a default validation context which holds the list of
	//      matchers for accepted SANs.
	//    - contains certificate provider instance configuration
	//  - certificate provider instance configuration
	//    - in this case, we do not get a list of accepted SANs.
	switch t := common.GetValidationContextType().(type) {
	case *v3tlspb.CommonTlsContext_CombinedValidationContext:
		combined := common.GetCombinedValidationContext()
		var matchers []matcher.StringMatcher
		if def := combined.GetDefaultValidationContext(); def != nil {
			for _, m := range def.GetMatchSubjectAltNames() {
				matcher, err := matcher.StringMatcherFromProto(m)
				if err != nil {
					return nil, err
				}
				matchers = append(matchers, matcher)
			}
		}
		if server && len(matchers) != 0 {
			return nil, fmt.Errorf("match_subject_alt_names field in validation context is not supported on the server: %v", common)
		}
		sc.SubjectAltNameMatchers = matchers
		if pi := combined.GetValidationContextCertificateProviderInstance(); pi != nil {
			sc.RootInstanceName = pi.GetInstanceName()
			sc.RootCertName = pi.GetCertificateName()
		}
	case *v3tlspb.CommonTlsContext_ValidationContextCertificateProviderInstance:
		pi := common.GetValidationContextCertificateProviderInstance()
		sc.RootInstanceName = pi.GetInstanceName()
		sc.RootCertName = pi.GetCertificateName()
	case nil:
		// It is valid for the validation context to be nil on the server side.
	default:
		return nil, fmt.Errorf("validation context contains unexpected type: %T", t)
	}
	return sc, nil
}

// gRFC A29 https://github.com/grpc/proposal/blob/master/A29-xds-tls-security.md
// specifies the new way to fetch security configuration and says the following:
//
// Although there are various ways to obtain certificates as per this proto
// (which are supported by Envoy), gRPC supports only one of them and that is
// the `CertificateProviderPluginInstance` proto.
//
// This helper function attempts to fetch security configuration from the
// `CertificateProviderPluginInstance` message, given a CommonTlsContext.
func securityConfigFromCommonTLSContextUsingNewFields(common *v3tlspb.CommonTlsContext, server bool) (*SecurityConfig, error) {
	// The `tls_certificate_provider_instance` field of type
	// `CertificateProviderPluginInstance` is used to fetch the identity
	// certificate provider.
	sc := &SecurityConfig{}
	identity := common.GetTlsCertificateProviderInstance()
	if identity == nil && len(common.GetTlsCertificates()) != 0 {
		return nil, fmt.Errorf("expected field tls_certificate_provider_instance is not set, while unsupported field tls_certificates is set in CommonTlsContext message: %+v", common)
	}
	if identity == nil && common.GetTlsCertificateSdsSecretConfigs() != nil {
		return nil, fmt.Errorf("expected field tls_certificate_provider_instance is not set, while unsupported field tls_certificate_sds_secret_configs is set in CommonTlsContext message: %+v", common)
	}
	sc.IdentityInstanceName = identity.GetInstanceName()
	sc.IdentityCertName = identity.GetCertificateName()

	// The `CommonTlsContext` contains a oneof field `validation_context_type`,
	// which contains the `CertificateValidationContext` message in one of the
	// following ways:
	//  - `validation_context` field
	//    - this is directly of type `CertificateValidationContext`
	//  - `combined_validation_context` field
	//    - this is of type `CombinedCertificateValidationContext` and contains
	//      a `default validation context` field of type
	//      `CertificateValidationContext`
	//
	// The `CertificateValidationContext` message has the following fields that
	// we are interested in:
	//  - `ca_certificate_provider_instance`
	//    - this is of type `CertificateProviderPluginInstance`
	//  - `system_root_certs`:
	//    - This indicates the usage of system root certs for validation.
	//  - `match_subject_alt_names`
	//    - this is a list of string matchers
	//
	// The `CertificateProviderPluginInstance` message contains two fields
	//  - instance_name
	//    - this is the certificate provider instance name to be looked up in
	//      the bootstrap configuration
	//  - certificate_name
	//    -  this is an opaque name passed to the certificate provider
	var validationCtx *v3tlspb.CertificateValidationContext
	switch typ := common.GetValidationContextType().(type) {
	case *v3tlspb.CommonTlsContext_ValidationContext:
		validationCtx = common.GetValidationContext()
	case *v3tlspb.CommonTlsContext_CombinedValidationContext:
		validationCtx = common.GetCombinedValidationContext().GetDefaultValidationContext()
	case nil:
		// It is valid for the validation context to be nil on the server side.
		return sc, nil
	default:
		return nil, fmt.Errorf("validation context contains unexpected type: %T", typ)
	}
	// If we get here, it means that the `CertificateValidationContext` message
	// was found through one of the supported ways. It is an error if the
	// validation context is specified, but it does not specify a way to
	// validate TLS certificates. Peer TLS certs can be verified in the
	// following ways:
	// 1. If the ca_certificate_provider_instance field is set, it contains
	//    information about the certificate provider to be used for the root
	//    certificates, else
	// 2. If the system_root_certs field is set, and the config is for a client,
	//    use the system default root certs.
	useSystemRootCerts := false
	if validationCtx.GetCaCertificateProviderInstance() == nil {
		if server {
			if validationCtx.GetSystemRootCerts() != nil {
				// The `system_root_certs` field will not be supported on the
				// gRPC server side. If `ca_certificate_provider_instance` is
				// unset and `system_root_certs` is set, the LDS resource will
				// be NACKed.
				// - A82
				return nil, fmt.Errorf("expected field ca_certificate_provider_instance is missing and unexpected field system_root_certs is set for server in CommonTlsContext message: %+v", common)
			}
		} else {
			if validationCtx.GetSystemRootCerts() != nil {
				useSystemRootCerts = true
			}
		}
	}

	// The following fields are ignored:
	// - trusted_ca
	// - watched_directory
	// - allow_expired_certificate
	// - trust_chain_verification
	switch {
	case len(validationCtx.GetVerifyCertificateSpki()) != 0:
		return nil, fmt.Errorf("unsupported verify_certificate_spki field in CommonTlsContext message: %+v", common)
	case len(validationCtx.GetVerifyCertificateHash()) != 0:
		return nil, fmt.Errorf("unsupported verify_certificate_hash field in CommonTlsContext message: %+v", common)
	case validationCtx.GetRequireSignedCertificateTimestamp().GetValue():
		return nil, fmt.Errorf("unsupported require_signed_certificate_timestamp field in CommonTlsContext message: %+v", common)
	case validationCtx.GetCrl() != nil:
		return nil, fmt.Errorf("unsupported crl field in CommonTlsContext message: %+v", common)
	case validationCtx.GetCustomValidatorConfig() != nil:
		return nil, fmt.Errorf("unsupported custom_validator_config field in CommonTlsContext message: %+v", common)
	}

	if rootProvider := validationCtx.GetCaCertificateProviderInstance(); rootProvider != nil {
		sc.RootInstanceName = rootProvider.GetInstanceName()
		sc.RootCertName = rootProvider.GetCertificateName()
	} else if useSystemRootCerts {
		sc.UseSystemRootCerts = true
	} else if !server && envconfig.XDSSystemRootCertsEnabled {
		return nil, fmt.Errorf("expected fields ca_certificate_provider_instance and system_root_certs are missing in CommonTlsContext message: %+v", common)
	} else {
		// Don't mention the system_root_certs field if it was not checked.
		return nil, fmt.Errorf("expected field ca_certificate_provider_instance is missing in CommonTlsContext message: %+v", common)
	}

	var matchers []matcher.StringMatcher
	for _, m := range validationCtx.GetMatchSubjectAltNames() {
		matcher, err := matcher.StringMatcherFromProto(m)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)
	}
	if server && len(matchers) != 0 {
		return nil, fmt.Errorf("match_subject_alt_names field in validation context is not supported on the server: %v", common)
	}
	sc.SubjectAltNameMatchers = matchers
	return sc, nil
}
