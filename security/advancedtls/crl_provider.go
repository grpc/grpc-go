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
	"sync"
	"time"
)

const defaultCRLRefreshDuration = 1 * time.Hour

// CRLProvider is the interface to be implemented to enable custom CRL provider
// behavior.
//
// The interface defines how the data is read, but doesn't prescribe a way
// CRL are loaded and stored. Such implementations can be used in
// RevocationConfig of advancedtls.ClientOptions and/or
// advancedtls.ServerOptions .
//
// TODO(erm-g): Add link to related gRFC once it's ready.
// Please refer to https://github.com/grpc/proposal/ for more details.
type CRLProvider interface {
	// CRL accepts x509 Cert and returns back related CRL struct. The CRL struct
	// can be nil, can contain empty or non-empty list of revkoed certificates.
	// Callers are expected to use the returned value as read-only.
	CRL(cert *x509.Certificate) (*CRL, error)
}

// StaticCRLProvider implements CRLProvider interface by accepting raw content
// of CRL files at creation time and storing parsed CRL structs  in-memory.
type StaticCRLProvider struct {
	// TODO CRL is sort of our internal representation - provide an API for
	// people to read into it, or provide a simpler type in the API then
	// internally convert to this form
	crls map[string]*CRL
}

// MakeStaticCRLProvider processes raw content of CRL files, adds parsed CRL
// structs into in-memory, and returns a new instance of the StaticCRLProvider.
func MakeStaticCRLProvider(rawCRLs [][]byte) *StaticCRLProvider {
	p := StaticCRLProvider{}
	p.crls = make(map[string]*CRL)
	for idx, rawCRL := range rawCRLs {
		cRL, err := NewCRL(rawCRL)
		if err != nil {
			grpclogLogger.Warningf("Can't parse raw CRL number %v from the slice: %v", idx, err)
			continue
		}
		p.addCRL(cRL)
	}
	return &p
}

// AddCRL adds/updates provided CRL to in-memory storage.
func (p *StaticCRLProvider) addCRL(crl *CRL) {
	key := crl.CertList.Issuer.ToRDNSequence().String()
	p.crls[key] = crl
}

// CRL returns CRL struct if it was previously loaded by calling AddCRL.
func (p *StaticCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	return p.crls[cert.Issuer.ToRDNSequence().String()], nil
}

// Options represents a data structure holding a
// configuration for FileWatcherCRLProvider.
type Options struct {
	CRLDirectory               string          // Path of the directory containing CRL files
	RefreshDuration            time.Duration   // Time interval between CRLDirectory scans
	cRLReloadingFailedCallback func(err error) // Custom callback executed when a CRL file can’t be processed
}

// FileWatcherCRLProvider implements the CRLProvider interface by periodically scanning
// CRLDirectory (see Options) and storing CRL structs in-memory
type FileWatcherCRLProvider struct {
	crls   map[string]*CRL
	opts   Options
	mu     sync.Mutex
	cancel context.CancelFunc
}

// MakeFileWatcherCRLProvider returns a new instance of the
// FileWatcherCRLProvider. It uses Options to validate and apply configuration
// required for creating a new instance.
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
		}
		return err
	}
	if !fileInfo.IsDir() {
		return fmt.Errorf("advancedtls: CRLDirectory %v is not a directory", o.CRLDirectory)
	}
	_, err = os.Open(o.CRLDirectory)
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("advancedtls: CRLDirectory %v is not readable", o.CRLDirectory)
		}
		return err
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
	p.ScanCRLDirectory()

	for {
		select {
		case <-ctx.Done():
			ticker.Stop()
			return
		case <-ticker.C:
			p.ScanCRLDirectory()
		}
	}
}

// Close stops the background refresh of CRLDirectory of FileWatcherCRLProvider
func (p *FileWatcherCRLProvider) Close() {
	p.cancel()
}

// ScanCRLDirectory starts the process of scanning Options.CRLDirectory and
// updating in-memory storage of CRL structs.Please note that the same method is
// called periodically by run goroutine.
func (p *FileWatcherCRLProvider) ScanCRLDirectory() {
	dir, err := os.Open(p.opts.CRLDirectory)
	if err != nil {
		grpclogLogger.Errorf("Can't open CRLDirectory %v", p.opts.CRLDirectory, err)
		if p.opts.cRLReloadingFailedCallback != nil {
			p.opts.cRLReloadingFailedCallback(err)
		}
	}
	defer dir.Close()

	files, err := dir.ReadDir(0)
	if err != nil {
		grpclogLogger.Errorf("Can't access files under CRLDirectory %v", p.opts.CRLDirectory, err)
		if p.opts.cRLReloadingFailedCallback != nil {
			p.opts.cRLReloadingFailedCallback(err)
		}
	}

	successCounter := 0
	failCounter := 0
	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", p.opts.CRLDirectory, file.Name())
		err := p.addCRL(filePath)
		if err != nil {
			failCounter++
			grpclogLogger.Warningf("Can't add CRL from file %v under CRLDirectory %v", filePath, p.opts.CRLDirectory, err)
			if p.opts.cRLReloadingFailedCallback != nil {
				p.opts.cRLReloadingFailedCallback(err)
			}
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
	p.mu.Lock()
	defer p.mu.Unlock()
	p.crls[key] = certList
	grpclogLogger.Infof("In-memory CRL storage of FileWatcherCRLProvider for key %v updated", key)
	return nil
}

// CRL retrieves the CRL associated with the given certificate's issuer DN from
// in-memory if it was previously loaded during CRLDirectory scan.
func (p *FileWatcherCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.crls[cert.Issuer.ToRDNSequence().String()], nil
}
