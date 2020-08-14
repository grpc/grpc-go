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
	"os"
	"time"

	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/grpclog"
)

const (
	defaultInterval = 1 * time.Hour
)

var logger = grpclog.Component("advancedtls")

// IdentityPEMFileProviderOptions contains fields to be filled out by users
// for obtaining identity private key and certificates from PEM files.
type IdentityPEMFileProviderOptions struct {
	// CertFile is the file path that holds certificate file specified by users
	// whose updates will be captured by a watching goroutine.
	CertFile string
	// KeyFile is the file path that holds private key specified by users
	// whose updates will be captured by a watching goroutine.
	KeyFile string
	// The identity files will be periodically updated for the duration of Interval.
	// The default Interval is set to 1 hour if users did not specify this field.
	Interval time.Duration
}

// IdentityPEMFileProvider implements certprovider.Provider.
// It provides the most up-to-date identity private key
// and certificates based on the input PEM files.
type IdentityPEMFileProvider struct {
	distributor *certprovider.Distributor
	cancel      context.CancelFunc
}

// RootPEMFileProviderOptions contains fields to be filled out by users
// for obtaining root certificates from PEM files.
type RootPEMFileProviderOptions struct {
	// TrustFile is the file path that holds trust file specified by users
	// whose updates will be captured by a watching goroutine.
	TrustFile string
	// The trust files will be periodically updated for the duration of Interval.
	// The default Interval is set to 1 hour if users did not specify this field.
	Interval time.Duration
}

// RootPEMFileProvider implements certprovider.Provider.
// It provides the most up-to-date root certificates based on the input PEM files.
type RootPEMFileProvider struct {
	distributor *certprovider.Distributor
	cancel      context.CancelFunc
}

// NewIdentityPEMFileProvider uses IdentityPEMFileProviderOptions to construct a IdentityPEMFileProvider.
func NewIdentityPEMFileProvider(o *IdentityPEMFileProviderOptions) (*IdentityPEMFileProvider, error) {
	if o.CertFile == "" {
		return nil, fmt.Errorf("users must specify CertFile in IdentityPEMFileProviderOptions")
	}
	if o.KeyFile == "" {
		return nil, fmt.Errorf("users must specify KeyFile in IdentityPEMFileProviderOptions")
	}
	// If interval is not set by users explicitly, we will set it to default interval.
	if o.Interval == 0 {
		o.Interval = defaultInterval
	}
	ticker := time.NewTicker(o.Interval)
	ctx, cancel := context.WithCancel(context.Background())
	// Initialize the distributor of provider with an empty KeyMaterial.
	provider := &IdentityPEMFileProvider{distributor: certprovider.NewDistributor()}
	provider.distributor.Set(&certprovider.KeyMaterial{}, nil)
	// A goroutine to pull file changes.
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				{
					// Read identity certs from PEM files.
					identityCert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
					if err != nil {
						// If the reading produces an error, we will skip the update for this round and log the error.
						// Note that LoadX509KeyPair will return error if file is empty,
						// so there is no separate check for empty file contents.
						logger.Warning("tls.LoadX509KeyPair(%v, %v) failed: %v", o.CertFile, o.KeyFile, err)
						continue
					}
					provider.distributor.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}, nil)
				}
			default:
			}
		}
	}(ctx)
	provider.cancel = cancel
	return provider, nil
}

// KeyMaterial returns the key material sourced by the IdentityPEMFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *IdentityPEMFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the IdentityPEMFileProvider.
func (p *IdentityPEMFileProvider) Close() {
	p.cancel()
	p.distributor.Stop()
}

// NewRootPEMFileProvider uses RootPEMFileProviderOptions to construct a RootPEMFileProvider.
func NewRootPEMFileProvider(o *RootPEMFileProviderOptions) (*RootPEMFileProvider, error) {
	if o.TrustFile == "" {
		return nil, fmt.Errorf("users must specify TrustFile in RootPEMFileProviderOptions")
	}
	// If interval is not set by users explicitly, we will set it to default interval.
	if o.Interval == 0 {
		o.Interval = defaultInterval
	}
	ticker := time.NewTicker(o.Interval)
	ctx, cancel := context.WithCancel(context.Background())
	// Initialize the distributor of provider with an empty KeyMaterial.
	provider := &RootPEMFileProvider{distributor: certprovider.NewDistributor()}
	provider.distributor.Set(&certprovider.KeyMaterial{}, nil)
	// A goroutine to pull file changes.
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				{
					if trustFileSize, _ := getFileSize(o.TrustFile); trustFileSize == 0 {
						// If the current file is empty, skip the update for this round.
						continue
					}
					// Read root certs from PEM files.
					trustData, err := ioutil.ReadFile(o.TrustFile)
					trustPool := x509.NewCertPool()
					trustPool.AppendCertsFromPEM(trustData)
					if err != nil {
						// If the reading produces an error, we will skip the update for this round and log the error.
						logger.Warning("ioutil.ReadFile(%v) failed: %v", o.TrustFile, err)
						continue
					}
					provider.distributor.Set(&certprovider.KeyMaterial{Roots: trustPool}, nil)
				}
			default:
			}
		}
	}(ctx)
	provider.cancel = cancel
	return provider, nil
}

// KeyMaterial returns the key material sourced by the RootPEMFileProvider.
// Callers are expected to use the returned value as read-only.
func (p *RootPEMFileProvider) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	return p.distributor.KeyMaterial(ctx)
}

// Close cleans up resources allocated by the RootPEMFileProvider.
func (p *RootPEMFileProvider) Close() {
	p.cancel()
	p.distributor.Stop()
}

func getFileSize(filepath string) (int64, error) {
	f, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return f.Size(), nil
}
