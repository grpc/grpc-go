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
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"time"
)

const defaultCRLRefreshDuration = 1 * time.Hour

type CRLProvider interface {
	// Callers are expected to use the returned value as read-only.
	CRL(cert *x509.Certificate) (*CRL, error)
}

type StaticCRLProvider struct {
	// TODO CRL is sort of our internal representation - provide an API for
	// people to read into it, or provide a simpler type in the API then
	// internally convert to this form
	crls map[string]*CRL
}

func MakeStaticCRLProvider() *StaticCRLProvider {
	p := StaticCRLProvider{}
	p.crls = make(map[string]*CRL)
	return &p
}

func (p *StaticCRLProvider) AddCRL(crl *CRL) {
	key := crl.CertList.Issuer.ToRDNSequence().String()
	p.crls[key] = crl
}

func (p *StaticCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	// TODO handle no CRL found
	key := cert.Issuer.ToRDNSequence().String()
	return p.crls[key], nil
}

type Options struct {
	CRLDirectory    string
	RefreshDuration time.Duration
}

// NewFileWatcherCRLProvider creates a new FileWatcherCRLProvider.
type FileWatcherCRLProvider struct {
	crls   map[string]*CRL
	opts   Options
	cancel context.CancelFunc
}

func MakeFileWatcherCRLProvider(o Options) (*FileWatcherCRLProvider, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	provider := &FileWatcherCRLProvider{
		crls: make(map[string]*CRL),
		opts: o,
	}
	provider.cancel = cancel
	go provider.run(ctx)
	return provider, nil
}

func (o *Options) validate() error {
	// Checks relates to CRLDirectory.
	if o.CRLDirectory == "" {
		return fmt.Errorf("advancedtls: CRLDirectory needs to be specified")
	}
	fileInfo, err := os.Stat(o.CRLDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("advancedtls: CRLDirectory %v does not exist", o.CRLDirectory)
		} else {
			return err
		}
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("advancedtls: CRLDirectory %v is not a directory", o.CRLDirectory)
	}
	_, err = os.Open(o.CRLDirectory)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("advancedtls: CRLDirectory %v is not readable:", o.CRLDirectory)
		} else {
			return err
		}
	}
	// Checks related to RefreshDuration.
	if o.RefreshDuration <= 0 || o.RefreshDuration < time.Second {
		o.RefreshDuration = defaultCRLRefreshDuration
		grpclogLogger.Warningf("RefreshDuration must larger then 1 second: provided value %v, default value will be used %v", o.RefreshDuration, defaultCRLRefreshDuration)
	}
	return nil
}

// Start starts watching the directory for CRL files and updates the provider accordingly.
func (p *FileWatcherCRLProvider) run(ctx context.Context) {
	ticker := time.NewTicker(p.opts.RefreshDuration)
	defer ticker.Stop()
	p.scanCRLDirectory()

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			p.scanCRLDirectory()
		}
	}
}

// Stop stops the CRL provider and releases resources.
func (p *FileWatcherCRLProvider) Close() {
	p.cancel()
}

func (p *FileWatcherCRLProvider) scanCRLDirectory() {
	dir, err := os.Open(p.opts.CRLDirectory)
	if err != nil {
		grpclogLogger.Errorf("Can't open CRLDirectory %v", p.opts.CRLDirectory, err)
	}
	defer dir.Close()

	files, err := dir.ReadDir(0)
	if err != nil {
		grpclogLogger.Errorf("Can't access files under CRLDirectory %v", p.opts.CRLDirectory, err)
	}

	successCounter := 0
	failCounter := 0
	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", p.opts.CRLDirectory, file.Name())
		err := p.addCRL(filePath)
		if err != nil {
			failCounter++
			grpclogLogger.Warningf("Can't add CRL from file %v under CRLDirectory %v", filePath, p.opts.CRLDirectory, err)
			continue
		}
		successCounter++
	}
	grpclogLogger.Infof("Scan of CRLDirectory %v completed, %v files tried, %v CRLs added, %v files failed", len(files), successCounter, failCounter)
}

func (p *FileWatcherCRLProvider) addCRL(filePath string) error {
	crlBytes, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	crl, err := parseRevocationList(crlBytes)
	if err != nil {
		return fmt.Errorf("addCRL: can't parse CRL from file %v: %v", filePath, err)
	}
	var certList *CRL
	if certList, err = parseCRLExtensions(crl); err != nil {
		return fmt.Errorf("addCRL: unsupported CRL %v: %v", filePath, err)
	}
	rawCRLIssuer, err := extractCRLIssuer(crlBytes)
	if err != nil {
		return fmt.Errorf("addCRL: can't extract Issuer from CRL from file %v: %v", filePath, err)
	}
	certList.RawIssuer = rawCRLIssuer
	key := certList.CertList.Issuer.ToRDNSequence().String()
	p.crls[key] = certList
	grpclogLogger.Infof("In-memory CRL storage of FileWatcherCRLProvider for key %v updated", key)
	return nil
}

// CRL retrieves the CRL associated with the given certificate's issuer DN.
func (p *FileWatcherCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	key := cert.Issuer.ToRDNSequence().String()
	return p.crls[key], nil
}
