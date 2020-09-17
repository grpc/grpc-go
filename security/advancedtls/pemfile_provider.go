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

package advancedtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"time"

	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/grpclog"
)

const defaultIdentityInterval = 1 * time.Hour
const defaultRootInterval = 2 * time.Hour

// readKeyCertPairFunc will be overridden from unit tests.
var readKeyCertPairFunc = tls.LoadX509KeyPair

// readTrustCertFunc will be overridden from unit tests.
var readTrustCertFunc = func(trustFile string) (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(trustFile)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	if !trustPool.AppendCertsFromPEM(trustData) {
		return nil, fmt.Errorf("AppendCertsFromPEM failed to parse certificates")
	}
	return trustPool, nil
}

var logger = grpclog.Component("advancedtls")

// PEMFileProviderOptions contains options to configure a PEMFileProvider.
// Note that these fields will only take effect during construction. Once the
// PEMFileProvider starts, changing fields in PEMFileProviderOptions will have
// no effect.
type PEMFileProviderOptions struct {
	// CertFile is the file path that holds identity certificate whose updates
	// will be captured by a watching goroutine.
	// Optional. If this is set, KeyFile must also be set.
	CertFile string
	// KeyFile is the file path that holds identity private key whose updates
	// will be captured by a watching goroutine.
	// Optional. If this is set, CertFile must also be set.
	KeyFile string
	// TrustFile is the file path that holds trust certificate whose updates will
	// be captured by a watching goroutine.
	// Optional.
	TrustFile string
	// IdentityInterval is the time duration between two credential update checks
	// for identity certs.
	// Optional. If not set, we will use the default interval(1 hour).
	IdentityInterval time.Duration
	// RootInterval is the time duration between two credential update checks
	// for root certs.
	// Optional. If not set, we will use the default interval(2 hours).
	RootInterval time.Duration
}

// PEMFileProvider implements certprovider.Provider.
// It provides the most up-to-date identity private key-cert pairs and/or
// root certificates.
type PEMFileProvider struct {
	identityDistributor *certprovider.Distributor
	rootDistributor     *certprovider.Distributor
	cancel              context.CancelFunc
}

// NewPEMFileProvider returns a new PEMFileProvider constructed using the
// provided options.
func NewPEMFileProvider(o PEMFileProviderOptions) (*PEMFileProvider, error) {
	if o.CertFile == "" && o.KeyFile == "" && o.TrustFile == "" {
		return nil, fmt.Errorf("at least one credential file needs to be specified")
	}
	if keySpecified, certSpecified := o.KeyFile != "", o.CertFile != ""; keySpecified != certSpecified {
		return nil, fmt.Errorf("private key file and identity cert file should be both specified or not specified")
	}
	if o.IdentityInterval == 0 {
		o.IdentityInterval = defaultIdentityInterval
	}
	if o.RootInterval == 0 {
		o.RootInterval = defaultRootInterval
	}
	provider := &PEMFileProvider{}
	if o.CertFile != "" && o.KeyFile != "" {
		provider.identityDistributor = certprovider.NewDistributor()
	}
	if o.TrustFile != "" {
		provider.rootDistributor = certprovider.NewDistributor()
	}
	// A goroutine to pull file changes.
	identityTicker := time.NewTicker(o.IdentityInterval)
	rootTicker := time.NewTicker(o.RootInterval)
	ctx, cancel := context.WithCancel(context.Background())
	// We pass a copy of PEMFileProviderOptions to the goroutine in case users
	// change it after we start reloading.
	go func() {
		for {
			select {
			case <-ctx.Done():
				identityTicker.Stop()
				rootTicker.Stop()
				return
			case <-identityTicker.C:
				if provider.identityDistributor == nil {
					continue
				}
				// Read identity certs from PEM files.
				identityCert, err := readKeyCertPairFunc(o.CertFile, o.KeyFile)
				if err != nil {
					// If the reading produces an error, we will skip the update for this
					// round and log the error.
					logger.Warningf("tls.LoadX509KeyPair reads %s and %s failed: %v", o.CertFile, o.KeyFile, err)
					continue
				}
				provider.identityDistributor.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}, nil)
			case <-rootTicker.C:
				if provider.rootDistributor == nil {
					continue
				}
				// Read root certs from PEM files.
				trustPool, err := readTrustCertFunc(o.TrustFile)
				if err != nil {
					// If the reading produces an error, we will skip the update for this
					// round and log the error.
					logger.Warningf("readTrustCertFunc reads %v failed: %v", o.TrustFile, err)
					continue
				}
				provider.rootDistributor.Set(&certprovider.KeyMaterial{Roots: trustPool}, nil)
			default:
			}
		}
	}()
	provider.cancel = cancel
	return provider, nil
}

// KeyMaterial returns the key material sourced by the PEMFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *PEMFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	km := &certprovider.KeyMaterial{}
	if p.identityDistributor != nil {
		identityKM, err := p.identityDistributor.KeyMaterial(ctx)
		if err != nil {
			return nil, err
		}
		km.Certs = identityKM.Certs
	}
	if p.rootDistributor != nil {
		rootKM, err := p.rootDistributor.KeyMaterial(ctx)
		if err != nil {
			return nil, err
		}
		km.Roots = rootKM.Roots
	}
	return km, nil
}

// Close cleans up resources allocated by the PEMFileProvider.
func (p *PEMFileProvider) Close() {
	p.cancel()
	if p.identityDistributor != nil {
		p.identityDistributor.Stop()
	}
	if p.rootDistributor != nil {
		p.rootDistributor.Stop()
	}
}
