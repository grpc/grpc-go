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
	"os"
	"time"

	"google.golang.org/grpc/credentials/tls/certprovider"
	"google.golang.org/grpc/grpclog"
)

const (
	defaultInterval = 1 * time.Hour
)

var logger = grpclog.Component("advancedtls")

// PEMFileProviderOptions contains fields to be filled out by users
// for obtaining identity private key, peer certificates and root certificates from PEM files.
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
	// The identity files will be periodically updated for the duration of Interval.
	// The default Interval is set to 1 hour if users did not specify this field.
	IdentityInterval time.Duration
	// The trust files will be periodically updated for the duration of Interval.
	// The default Interval is set to 1 hour if users did not specify this field.
	RootInterval time.Duration
}

// PEMFileProvider implements certprovider.Provider.
// It provides the most up-to-date identity private key, peer certificates
// and root certificates based on the input PEM files.
type PEMFileProvider struct {
	*certprovider.Distributor
	cancel context.CancelFunc
}

// NewPEMFileProvider uses PEMFileProviderOptions to construct a PEMFileProvider.
func NewPEMFileProvider(o *PEMFileProviderOptions) (*PEMFileProvider, error) {
	var identityUpdate = false
	var rootUpdate = false
	if o.CertFile != "" && o.KeyFile != "" {
		identityUpdate = true
	} else if o.CertFile != "" || o.KeyFile != "" {
		logger.Warning("users must specify both KeyFile and CertFile to update identity credentials")
	}
	if o.TrustFile != "" {
		rootUpdate = true
	}
	if o.IdentityInterval == 0 {
		// If IdentityInterval is not set by users explicitly, we will set it to default interval.
		o.IdentityInterval = defaultInterval
	}
	if o.RootInterval == 0 {
		// If RootInterval is not set by users explicitly, we will set it to default interval.
		o.RootInterval = defaultInterval
	}
	identityTicker := time.NewTicker(o.IdentityInterval)
	rootTicker := time.NewTicker(o.RootInterval)
	ctx, cancel := context.WithCancel(context.Background())
	provider := &PEMFileProvider{Distributor: certprovider.NewDistributor()}
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
				provider.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{identityCert}}, nil)
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
					continue
				}
				trustPool := x509.NewCertPool()
				trustPool.AppendCertsFromPEM(trustData)
				provider.Set(&certprovider.KeyMaterial{Roots: trustPool}, nil)
			default:
			}
		}
	}(ctx)
	provider.cancel = cancel
	return provider, nil
}

// Close cleans up resources allocated by the PEMFileProvider.
func (p *PEMFileProvider) Close() {
	p.cancel()
	p.Stop()
}

func getFileSize(filepath string) (int64, error) {
	f, err := os.Stat(filepath)
	if err != nil {
		return 0, err
	}
	return f.Size(), nil
}
