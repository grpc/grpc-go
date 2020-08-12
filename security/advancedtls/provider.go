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
)

const (
	defaultInterval = 1 * time.Hour
)

// IdentityPemFileProviderOptions contains fields to be filled out by users
// for obtaining identity private key and certificates from PEM files.
type IdentityPemFileProviderOptions struct {
	CertFile string
	KeyFile  string
	Interval time.Duration
}

// IdentityPemFileProvider implements certprovider.Provider.
// It provides the most up-to-date identity private key
// and certificates based on the input PEM files.
type IdentityPemFileProvider struct {
	distributor *certprovider.Distributor
	closeFunc   func()
}

// RootPemFileProviderOptions contains fields to be filled out by users
// for obtaining root certificates from PEM files.
type RootPemFileProviderOptions struct {
	TrustFile string
	Interval  time.Duration
}

// RootPemFileProvider implements certprovider.Provider.
// It provides the most up-to-date root certificates based on the input PEM files.
type RootPemFileProvider struct {
	distributor *certprovider.Distributor
	closeFunc   func()
}

// NewIdentityPemFileProvider uses IdentityPemFileProviderOptions to construct a IdentityPemFileProvider.
func NewIdentityPemFileProvider(o *IdentityPemFileProviderOptions, quit chan bool) (*IdentityPemFileProvider, error) {
	if o.CertFile == "" {
		return nil, fmt.Errorf("users need to specify certificate file path in NewIdentityPemFileProvider")
	}
	if o.KeyFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in NewIdentityPemFileProvider")
	}
	// If interval is not set by users explicitly, set it to default interval.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = defaultInterval
	}
	provider := &IdentityPemFileProvider{}
	provider.distributor = certprovider.NewDistributor()
	// A goroutine to pull file changes.
	go func(quit chan bool) {
		for {
			// Read identity certs from PEM files.
			identityCert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
			km := certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}
			provider.distributor.Set(&km, err)
			time.Sleep(o.Interval)
			select {
			case <-quit:
				return
			default:
			}
		}
	}(quit)
	provider.closeFunc = func() {
		close(quit)
	}
	return provider, nil
}

// KeyMaterial returns the key material sourced by the IdentityPemFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *IdentityPemFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the IdentityPemFileProvider.
func (p *IdentityPemFileProvider) Close() {
	p.closeFunc()
	p.distributor.Stop()
}

// NewRootPemFileProvider uses RootPemFileProviderOptions to construct a RootPemFileProvider.
func NewRootPemFileProvider(o *RootPemFileProviderOptions, quit chan bool) (*RootPemFileProvider, error) {
	if o.TrustFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in RootPemFileProviderOptions")
	}
	// If interval is not set by users explicitly, set it to default interval.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = defaultInterval
	}
	provider := &RootPemFileProvider{}
	provider.distributor = certprovider.NewDistributor()
	// A goroutine to pull file changes.
	go func(quit chan bool) {
		for {
			// Read root certs from PEM files.
			trustData, err := ioutil.ReadFile(o.TrustFile)
			trustPool := x509.NewCertPool()
			trustPool.AppendCertsFromPEM(trustData)
			km := certprovider.KeyMaterial{Roots: trustPool}
			provider.distributor.Set(&km, err)
			time.Sleep(o.Interval)
			select {
			case <-quit:
				return
			default:
			}
		}
	}(quit)
	provider.closeFunc = func() {
		close(quit)
	}
	return provider, nil
}

// KeyMaterial returns the key material sourced by the RootPemFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *RootPemFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the RootPemFileProvider.
func (p *RootPemFileProvider) Close() {
	p.closeFunc()
	p.distributor.Stop()
}
