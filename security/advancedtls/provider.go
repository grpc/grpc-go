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
	defaultTestTimeout = 1 * time.Second
)

// IdentityPemFileProviderOptions contains fields to be filled out by users
// for obtaining identity credential files.
type IdentityPemFileProviderOptions struct {
	CertFile string
	KeyFile  string
	Interval time.Duration
}

// IdentityPemFileProvider keeps identity credential implementations up to
// date with secrets that they rely on to secure communications on the
// underlying channel.
type IdentityPemFileProvider struct {
	distributor *certprovider.Distributor
	closeFunc   func()
}

// RootPemFileProviderOptions contains fields to be filled out by users
// for obtaining root credential files.
type RootPemFileProviderOptions struct {
	TrustFile string
	Interval  time.Duration
}

// RootPemFileProvider keeps root credential implementations up to
// date with secrets that they rely on to secure communications on the
// underlying channel.
type RootPemFileProvider struct {
	distributor *certprovider.Distributor
	closeFunc   func()
}

// NewIdentityPemFileProvider uses IdentityPemFileProviderOptions to
// construct a IdentityPemFileProvider.
func NewIdentityPemFileProvider(o *IdentityPemFileProviderOptions) (*IdentityPemFileProvider, error) {
	if o.CertFile == "" {
		return nil, fmt.Errorf("users need to specify certificate file path in NewIdentityPemFileProvider")
	}
	if o.KeyFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in NewIdentityPemFileProvider")
	}
	// If interval is not set by users explicitly, set it to 1 hour by default.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = 1 * time.Hour
	}
	provider := &IdentityPemFileProvider{}
	quit := make(chan bool)
	provider.distributor = certprovider.NewDistributor()
	// A goroutine to pull file changes.
	go func() {
		for {
			// Read identity certs from PEM files.
			identityCert, err := o.ReadKeyAndCerts()
			km := certprovider.KeyMaterial{Certs: []tls.Certificate{*identityCert}}
			provider.distributor.Set(&km, err)
			time.Sleep(o.Interval)
			select {
			case <-quit:
				return
			default:
			}
		}
	}()
	provider.closeFunc = func() {
		close(quit)
	}
	quit <- true
	return provider, nil
}

// KeyMaterial returns the key material sourced by the provider.
// Callers are expected to use the returned value as read-only.
func (p *IdentityPemFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the provider.
func (p *IdentityPemFileProvider) Close() {
	p.closeFunc()
	p.distributor.Stop()
}

// NewRootPemFileProvider uses RootPemFileProviderOptions to
// construct a RootPemFileProvider.
func NewRootPemFileProvider(o *RootPemFileProviderOptions) (*RootPemFileProvider, error) {
	if o.TrustFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in RootPemFileProviderOptions")
	}
	// If interval is not set by users explicitly, set it to 1 hour by default.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = 1 * time.Hour
	}
	provider := &RootPemFileProvider{}
	quit := make(chan bool)
	provider.distributor = certprovider.NewDistributor()
	// go routine to pull file changes
	go func() {
		for {
			// reading identity certs from PEM files
			rootCert, err := o.ReadTrustCerts()
			km := certprovider.KeyMaterial{Roots: rootCert}
			provider.distributor.Set(&km, err)
			time.Sleep(o.Interval)
			select {
			case <-quit:
				return
			default:
			}
		}
	}()
	provider.closeFunc = func() {
		close(quit)
	}
	quit <- true
	return provider, nil
}

// KeyMaterial returns the key material sourced by the provider.
// Callers are expected to use the returned value as read-only.
func (p *RootPemFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the provider.
func (p *RootPemFileProvider) Close() {
	p.closeFunc()
	p.distributor.Stop()
}

// ReadKeyAndCerts reads and parses peer certificates from the certificate file path
// and the key file path specified in IdentityPemFileProviderOptions.
func (r IdentityPemFileProviderOptions) ReadKeyAndCerts() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(r.CertFile, r.KeyFile)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// ReadTrustCerts reads and parses trust certificates from trust certificate path
// specified in RootPemFileProviderOptions.
func (r RootPemFileProviderOptions) ReadTrustCerts() (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(r.TrustFile)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	trustPool.AppendCertsFromPEM(trustData)
	return trustPool, nil
}
