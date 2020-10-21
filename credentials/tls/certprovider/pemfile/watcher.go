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

// Package pemfile provides a file watching certificate provider plugin
// implementation which works for files with PEM contents.
package pemfile

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
	defaultCertRefreshDuration = 1 * time.Hour
	defaultRootRefreshDuration = 2 * time.Hour
)

var (
	// For overriding from unit tests.
	readKeyCertPairFunc = tls.LoadX509KeyPair
	readTrustCertFunc   = func(trustFile string) (*x509.CertPool, error) {
		trustData, err := ioutil.ReadFile(trustFile)
		if err != nil {
			return nil, err
		}
		trustPool := x509.NewCertPool()
		if !trustPool.AppendCertsFromPEM(trustData) {
			return nil, fmt.Errorf("pemfile: failed to parse root certificate")
		}
		return trustPool, nil
	}
	newDistributorFunc = func() distributor { return certprovider.NewDistributor() }
	logger             = grpclog.Component("pemfile")
)

// Options configures a certificate provider plugin that watches a specified set
// of files that contain certificates and keys in PEM format.
type Options struct {
	// CertFile is the file that holds the identity certificate.
	// Optional. If this is set, KeyFile must also be set.
	CertFile string
	// KeyFile is the file that holds identity private key.
	// Optional. If this is set, CertFile must also be set.
	KeyFile string
	// RootFile is the file that holds trusted root certificate(s).
	// Optional.
	RootFile string
	// CertRefreshDuration is the amount of time the plugin waits before
	// checking for updates in the specified identity certificate and key file.
	// Optional. If not set, a default value (1 hour) will be used.
	CertRefreshDuration time.Duration
	// RootRefreshDuration is the amount of time the plugin waits before
	// checking for updates in the specified root file.
	// Optional. If not set, a default value (2 hour) will be used.
	RootRefreshDuration time.Duration
}

// NewProvider returns a new certificate provider plugin that is configured to
// watch the PEM files specified in the passed in options.
func NewProvider(o Options) (certprovider.Provider, error) {
	if o.CertFile == "" && o.KeyFile == "" && o.RootFile == "" {
		return nil, fmt.Errorf("pemfile: at least one credential file needs to be specified")
	}
	if keySpecified, certSpecified := o.KeyFile != "", o.CertFile != ""; keySpecified != certSpecified {
		return nil, fmt.Errorf("pemfile: private key file and identity cert file should be both specified or not specified")
	}
	if o.CertRefreshDuration == 0 {
		o.CertRefreshDuration = defaultCertRefreshDuration
	}
	if o.RootRefreshDuration == 0 {
		o.RootRefreshDuration = defaultRootRefreshDuration
	}

	provider := &watcher{opts: o}
	if o.CertFile != "" && o.KeyFile != "" {
		provider.identityDistributor = newDistributorFunc()
	}
	if o.RootFile != "" {
		provider.rootDistributor = newDistributorFunc()
	}

	ctx, cancel := context.WithCancel(context.Background())
	provider.cancel = cancel
	go provider.run(ctx)

	return provider, nil
}

// watcher is a certificate provider plugin that implements the
// certprovider.Provider interface. It watches a set of certificate and key
// files and provides the most up-to-date key material for consumption by
// credentials implementation.
type watcher struct {
	identityDistributor distributor
	rootDistributor     distributor
	opts                Options
	certFileLastModTime time.Time
	keyFileLastModTime  time.Time
	rootFileLastModTime time.Time
	cancel              context.CancelFunc
}

// distributor wraps the methods on certprovider.Distributor which are used by
// the plugin. This is very useful in tests which need to know exactly when the
// plugin updates its key material.
type distributor interface {
	KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error)
	Set(km *certprovider.KeyMaterial, err error)
	Stop()
}

// updateIdentityDistributor checks if the cert/key files that the plugin is
// watching have changed, and if so, reads the new contents and updates the
// identityDistributor with the new key material.
func (w *watcher) updateIdentityDistributor() {
	if w.identityDistributor == nil {
		return
	}

	var (
		certFileModTime, keyFileModTime time.Time
		err                             error
	)
	certFileModTime, err = readFileModTime(w.opts.CertFile)
	if err != nil {
		logger.Warning(err)
		return
	}
	keyFileModTime, err = readFileModTime(w.opts.KeyFile)
	if err != nil {
		logger.Warning(err)
		return
	}
	if certFileModTime == w.certFileLastModTime && keyFileModTime == w.keyFileLastModTime {
		// Files have not changed from the last round. Do nothing.
		return
	}

	cert, err := readKeyCertPairFunc(w.opts.CertFile, w.opts.KeyFile)
	if err != nil {
		// Skip updates when cert/key pair parsing fails, and log the error.
		logger.Warningf("tls.LoadX509KeyPair(%q, %q) failed: %v", w.opts.CertFile, w.opts.KeyFile, err)
		return
	}

	// Update the last modification time only after successfully parsing the
	// cert. The cert/key pair will not parse successfully if we catch them in
	// the middle of an update.
	w.certFileLastModTime = certFileModTime
	w.keyFileLastModTime = keyFileModTime
	w.identityDistributor.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{cert}}, nil)
}

// updateRootDistributor checks if the root cert file that the plugin is
// watching hs changed, and if so, reads the new contents and updates the
// rootDistributor with the new key material.
func (w *watcher) updateRootDistributor() {
	if w.rootDistributor == nil {
		return
	}

	rootFileModTime, err := readFileModTime(w.opts.RootFile)
	if err != nil {
		logger.Warning(err)
		return
	}
	if rootFileModTime == w.rootFileLastModTime {
		// File has not changed from the last round. Do nothing.
		return
	}

	trustPool, err := readTrustCertFunc(w.opts.RootFile)
	if err != nil {
		// Skip updates when root cert parsing fails, and log the error.
		logger.Warningf("readTrustCertFunc reads %v failed: %v", w.opts.RootFile, err)
		return
	}

	// Update the last modification time only after successful parsing.
	w.rootFileLastModTime = rootFileModTime
	w.rootDistributor.Set(&certprovider.KeyMaterial{Roots: trustPool}, nil)
}

// run is a long running goroutine which watches the configured files for
// changes, and pushes new key material into the appropriate distributors which
// is returned from calls to KeyMaterial().
func (w *watcher) run(ctx context.Context) {
	// Update both root and identity certs at the beginning. Subsequently,
	// update only the appropriate file whose ticker has fired.
	w.updateIdentityDistributor()
	w.updateRootDistributor()

	identityTicker := time.NewTicker(w.opts.CertRefreshDuration)
	rootTicker := time.NewTicker(w.opts.RootRefreshDuration)
	for {
		select {
		case <-ctx.Done():
			identityTicker.Stop()
			rootTicker.Stop()
			return
		case <-identityTicker.C:
			w.updateIdentityDistributor()
		case <-rootTicker.C:
			w.updateRootDistributor()
		}
	}
}

// KeyMaterial returns the key material sourced by the watcher.
// Callers are expected to use the returned value as read-only.
func (w *watcher) KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error) {
	km := &certprovider.KeyMaterial{}
	if w.identityDistributor != nil {
		identityKM, err := w.identityDistributor.KeyMaterial(ctx)
		if err != nil {
			return nil, err
		}
		km.Certs = identityKM.Certs
	}
	if w.rootDistributor != nil {
		rootKM, err := w.rootDistributor.KeyMaterial(ctx)
		if err != nil {
			return nil, err
		}
		km.Roots = rootKM.Roots
	}
	return km, nil
}

// Close cleans up resources allocated by the watcher.
func (w *watcher) Close() {
	w.cancel()
	if w.identityDistributor != nil {
		w.identityDistributor.Stop()
	}
	if w.rootDistributor != nil {
		w.rootDistributor.Stop()
	}
}

func readFileModTime(name string) (time.Time, error) {
	f, err := os.Stat(name)
	if err != nil {
		return time.Time{}, fmt.Errorf("pemfile: os.Stat(%q) failed: %v", name, err)
	}
	return f.ModTime(), nil
}
