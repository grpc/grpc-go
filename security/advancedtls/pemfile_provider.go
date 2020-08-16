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
"io/ioutil"
"time"

"google.golang.org/grpc/credentials/tls/certprovider"
"google.golang.org/grpc/grpclog"
)

const defaultInterval = 1 * time.Hour

var logger = grpclog.Component("advancedtls")

// PEMFileProviderOptions contains options to configure a PEMFileProvider.
type PEMFileProviderOptions struct {
	// CertFile is the file path that holds certificate file specified by users
	// whose updates will be captured by a watching goroutine.
	CertFile string
	// KeyFile is the file path that holds private key specified by users
	// whose updates will be captured by a watching goroutine.
	KeyFile string
	// TrustFile is the file path that holds trust file specified by users
	// whose updates will be captured by a watching goroutine.
	TrustFile string
	// The identity files will be periodically reloaded for the duration of IdentityInterval.
	// The default Interval is set to 1 hour if users did not specify this field.
	IdentityInterval time.Duration
	// The trust files will be periodically reloaded for the duration of RootInterval.
	// The default Interval is set to 1 hour if users did not specify this field.
	RootInterval time.Duration
}

// PEMFileProvider implements certprovider.Provider.
// It provides the most up-to-date identity private key/cert pair
// and root certificates based on the input PEM files.
type PEMFileProvider struct {
	identityDistributor *certprovider.Distributor
	rootDistributor     *certprovider.Distributor
	cancel              context.CancelFunc
}

// NewPEMFileProvider uses PEMFileProviderOptions to construct a PEMFileProvider.
func NewPEMFileProvider(o *PEMFileProviderOptions) (*PEMFileProvider, error) {
	var identityUpdate, rootUpdate bool
	if o.CertFile != "" && o.KeyFile != "" {
		identityUpdate = true
	} else if o.CertFile != "" || o.KeyFile != "" {
		logger.Warning("users must specify both KeyFile and CertFile to update identity credentials")
	}
	if o.TrustFile != "" {
		rootUpdate = true
	}
	if o.IdentityInterval == 0 {
		o.IdentityInterval = defaultInterval
	}
	if o.RootInterval == 0 {
		o.RootInterval = defaultInterval
	}
	identityTicker := time.NewTicker(o.IdentityInterval)
	rootTicker := time.NewTicker(o.RootInterval)
	ctx, cancel := context.WithCancel(context.Background())
	provider := &PEMFileProvider{
		identityDistributor: certprovider.NewDistributor(),
		rootDistributor:     certprovider.NewDistributor(),
	}
	// A goroutine to pull file changes.
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				identityTicker.Stop()
				rootTicker.Stop()
				return
			case <-identityTicker.C:
				if !identityUpdate {
					continue
				}
				// Read identity certs from PEM files.
				identityCert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
				if err != nil {
					// If the reading produces an error, we will skip the update for this round and log the error.
					// Note that LoadX509KeyPair will return error if file is empty,
					// so there is no separate check for empty file contents.
					logger.Warning("tls.LoadX509KeyPair(%v, %v) failed: %v", o.CertFile, o.KeyFile, err)
					continue
				}
				provider.identityDistributor.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}, nil)
			case <-rootTicker.C:
				if !rootUpdate {
					continue
				}
				// Read root certs from PEM files.
				trustData, err := ioutil.ReadFile(o.TrustFile)
				if err != nil {
					// If the reading produces an error, we will skip the update for this round and log the error.
					logger.Warning("ioutil.ReadFile(%v) failed: %v", o.TrustFile, err)
					continue
				}
				if len(trustData) == 0 {
					// If the current file is empty, skip the update for this round.
					logger.Warning("ioutil.ReadFile(%v) reads an empty file: %v", o.TrustFile, err)
					continue
				}
				trustPool := x509.NewCertPool()
				ok := trustPool.AppendCertsFromPEM(trustData)
				if !ok {
					logger.Warning("trustPool.AppendCertsFromPEM(trustData) failed")
					continue
				}
				provider.rootDistributor.Set(&certprovider.KeyMaterial{Roots: trustPool}, nil)
			default:
			}
		}
	}(ctx)
	provider.cancel = cancel
	return provider, nil
}

// KeyMaterial returns the key material sourced by the PEMFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *PEMFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	identityKM, err := p.identityDistributor.KeyMaterial(ctx)
	if err != nil {
		return nil, err
	}
	rootKM, err := p.rootDistributor.KeyMaterial(ctx)
	if err != nil {
		return nil, err
	}
	return &certprovider.KeyMaterial{Certs: identityKM.Certs, Roots: rootKM.Roots}, nil
}

// Close cleans up resources allocated by the PEMFileProvider.
func (p *PEMFileProvider) Close() {
	p.cancel()
	p.identityDistributor.Stop()
	p.rootDistributor.Stop()
}
