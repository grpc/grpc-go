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

var logger = grpclog.Component("channelz")

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
func NewIdentityPemFileProvider(o *IdentityPemFileProviderOptions) (*IdentityPemFileProvider, error) {
	if o.CertFile == "" {
		return nil, fmt.Errorf("users need to specify certificate file path in NewIdentityPemFileProvider")
	}
	if o.KeyFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in NewIdentityPemFileProvider")
	}
	// If interval is not set by users explicitly, we will set it to default interval.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = defaultInterval
	}
	quit := make(chan bool)
	provider := &IdentityPemFileProvider{}
	provider.distributor = certprovider.NewDistributor()
	// Initialize the distributor with an empty KeyMaterial.
	provider.distributor.Set(&certprovider.KeyMaterial{}, nil)
	// A goroutine to pull file changes.
	go func() {
		for {
			// Read identity certs from PEM files.
			identityCert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
			// If the reading produces an error, we will skip the update for this round and log the error.
			// Note that LoadX509KeyPair will return error if file is empty,
			// so there is no separate check for empty file contents.
			if err != nil {
				logger.Warning("tls.LoadX509KeyPair(%v, %v) failed: %v", o.CertFile, o.KeyFile, err)
			} else {
				km := certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}
				provider.distributor.Set(&km, nil)
			}
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
func NewRootPemFileProvider(o *RootPemFileProviderOptions) (*RootPemFileProvider, error) {
	if o.TrustFile == "" {
		return nil, fmt.Errorf("users need to specify key file path in RootPemFileProviderOptions")
	}
	// If interval is not set by users explicitly, we will set it to default interval.
	if o.Interval == 0*time.Nanosecond {
		o.Interval = defaultInterval
	}
	quit := make(chan bool)
	provider := &RootPemFileProvider{}
	provider.distributor = certprovider.NewDistributor()
	// Initialize the distributor with an empty KeyMaterial.
	provider.distributor.Set(&certprovider.KeyMaterial{}, nil)
	// A goroutine to pull file changes.
	go func() {
		for {
			// If the current file is empty, skip the update for this round.
			if trustFileSize, _ := getFileSize(o.TrustFile); trustFileSize != 0 {
				// Read root certs from PEM files.
				trustData, err := ioutil.ReadFile(o.TrustFile)
				trustPool := x509.NewCertPool()
				trustPool.AppendCertsFromPEM(trustData)
				// If the reading produces an error, we will skip the update for this round and log the error.
				if err != nil {
					logger.Warning("ioutil.ReadFile(%v) failed: %v", o.TrustFile, err)
				} else {
					km := certprovider.KeyMaterial{Roots: trustPool}
					provider.distributor.Set(&km, nil)
				}
			}
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

func getFileSize(filepath string) (int64, error) {
	f, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return f.Size(), nil
}
