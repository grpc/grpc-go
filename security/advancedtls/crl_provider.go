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

package advancedtls

import (
	"crypto/x509"
)

type CRLProvider interface {
	// Callers are expected to use the returned value as read-only.
	CRL(cert *x509.Certificate) (*CRL, error)
}

type StaticCRLProvider struct {
	crls map[string]*CRL
}

func (p *StaticCRLProvider) AddCRL(crl *CRL) {
	p.crls[crl.CertList.Issuer.ToRDNSequence().String()] = crl
}

func (p *StaticCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	// TODO what to do if no CRL found
	return p.crls[cert.Issuer.ToRDNSequence().String()], nil
}
