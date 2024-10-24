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
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"
)

const defaultCRLRefreshDuration = 1 * time.Hour
const minCRLRefreshDuration = 1 * time.Minute

// CRLProvider is the interface to be implemented to enable custom CRL provider
// behavior, as defined in [gRFC A69].
//
// The interface defines how gRPC gets CRLs from the provider during handshakes,
// but doesn't prescribe a specific way to load and store CRLs. Such
// implementations can be used in RevocationOptions of advancedtls.ClientOptions
// and/or advancedtls.ServerOptions.
// Please note that checking CRLs is directly on the path of connection
// establishment, so implementations of the CRL function need to be fast, and
// slow things such as file IO should be done asynchronously.
//
// [gRFC A69]: https://github.com/grpc/proposal/pull/382
type CRLProvider interface {
	// CRL accepts x509 Cert and returns a related CRL struct, which can contain
	// either an empty or non-empty list of revoked certificates. If an error is
	// thrown or (nil, nil) is returned, it indicates that we can't load any
	// authoritative CRL files (which may not necessarily be a problem). It's not
	// considered invalid to have no CRLs if there are no revocations for an
	// issuer. In such cases, the status of the check CRL operation is marked as
	// RevocationUndetermined, as defined in [RFC5280 - Undetermined].
	//
	// [RFC5280 - Undetermined]: https://datatracker.ietf.org/doc/html/rfc5280#section-6.3.3
	CRL(cert *x509.Certificate) (*CRL, error)
}

// StaticCRLProvider implements CRLProvider interface by accepting raw content
// of CRL files at creation time and storing parsed CRL structs in-memory.
type StaticCRLProvider struct {
	crls map[string]*CRL
}

// NewStaticCRLProvider processes raw content of CRL files, adds parsed CRL
// structs into in-memory, and returns a new instance of the StaticCRLProvider.
func NewStaticCRLProvider(rawCRLs [][]byte) *StaticCRLProvider {
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
	key := crl.certList.Issuer.ToRDNSequence().String()
	p.crls[key] = crl
}

// CRL returns CRL struct if it was passed to NewStaticCRLProvider.
func (p *StaticCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	return p.crls[cert.Issuer.ToRDNSequence().String()], nil
}

// FileWatcherOptions represents a data structure holding a configuration for
// FileWatcherCRLProvider.
type FileWatcherOptions struct {
	CRLDirectory               string          // Required: Path of the directory containing CRL files
	RefreshDuration            time.Duration   // Optional: Time interval (default 1 hour) between CRLDirectory scans, can't be smaller than 1 minute
	CRLReloadingFailedCallback func(err error) // Optional: Custom callback executed when a CRL file can’t be processed
}

// FileWatcherCRLProvider implements the CRLProvider interface by periodically
// scanning CRLDirectory (see FileWatcherOptions) and storing CRL structs
// in-memory. Users should call Close to stop the background refresh of
// CRLDirectory.
type FileWatcherCRLProvider struct {
	crls map[string]*CRL
	opts FileWatcherOptions
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
}

// NewFileWatcherCRLProvider returns a new instance of the
// FileWatcherCRLProvider. It uses FileWatcherOptions to validate and apply
// configuration required for creating a new instance. The initial scan of
// CRLDirectory is performed inside this function. Users should call Close to
// stop the background refresh of CRLDirectory.
func NewFileWatcherCRLProvider(o FileWatcherOptions) (*FileWatcherCRLProvider, error) {
	if err := o.validate(); err != nil {
		return nil, err
	}
	provider := &FileWatcherCRLProvider{
		crls: make(map[string]*CRL),
		opts: o,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	provider.scanCRLDirectory()
	go provider.run()
	return provider, nil
}

func (o *FileWatcherOptions) validate() error {
	// Checks relates to CRLDirectory.
	if o.CRLDirectory == "" {
		return fmt.Errorf("advancedtls: CRLDirectory needs to be specified")
	}
	if _, err := os.ReadDir(o.CRLDirectory); err != nil {
		return fmt.Errorf("advancedtls: CRLDirectory %v is not readable: %v", o.CRLDirectory, err)
	}
	// Checks related to RefreshDuration.
	if o.RefreshDuration == 0 {
		o.RefreshDuration = defaultCRLRefreshDuration
	}
	if o.RefreshDuration < minCRLRefreshDuration {
		grpclogLogger.Warningf("RefreshDuration must be at least 1 minute: provided value %v, minimum value %v will be used.", o.RefreshDuration, minCRLRefreshDuration)
		o.RefreshDuration = minCRLRefreshDuration
	}
	return nil
}

// Start starts watching the directory for CRL files and updates the provider accordingly.
func (p *FileWatcherCRLProvider) run() {
	defer close(p.done)
	ticker := time.NewTicker(p.opts.RefreshDuration)
	defer ticker.Stop()

	for {
		select {
		case <-p.stop:
			grpclogLogger.Infof("Scanning of CRLDirectory %v stopped", p.opts.CRLDirectory)
			return
		case <-ticker.C:
			p.scanCRLDirectory()
		}
	}
}

// Close waits till the background refresh of CRLDirectory of
// FileWatcherCRLProvider is done and then stops it.
func (p *FileWatcherCRLProvider) Close() {
	close(p.stop)
	<-p.done
}

// scanCRLDirectory starts the process of scanning
// FileWatcherOptions.CRLDirectory and updating in-memory storage of CRL
// structs, as defined in [gRFC A69]. It's called periodically
// (see FileWatcherOptions.RefreshDuration) by run goroutine.
//
// [gRFC A69]: https://github.com/grpc/proposal/pull/382
func (p *FileWatcherCRLProvider) scanCRLDirectory() {
	dir, err := os.Open(p.opts.CRLDirectory)
	if err != nil {
		grpclogLogger.Errorf("Can't open CRLDirectory %v", p.opts.CRLDirectory, err)
		if p.opts.CRLReloadingFailedCallback != nil {
			p.opts.CRLReloadingFailedCallback(err)
		}
	}
	defer dir.Close()

	files, err := dir.ReadDir(0)
	if err != nil {
		grpclogLogger.Errorf("Can't access files under CRLDirectory %v", p.opts.CRLDirectory, err)
		if p.opts.CRLReloadingFailedCallback != nil {
			p.opts.CRLReloadingFailedCallback(err)
		}
	}

	tempCRLs := make(map[string]*CRL)
	successCounter := 0
	failCounter := 0
	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", p.opts.CRLDirectory, file.Name())
		crl, err := ReadCRLFile(filePath)
		if err != nil {
			failCounter++
			grpclogLogger.Warningf("Can't add CRL from file %v under CRLDirectory %v", filePath, p.opts.CRLDirectory, err)
			if p.opts.CRLReloadingFailedCallback != nil {
				p.opts.CRLReloadingFailedCallback(err)
			}
			continue
		}
		tempCRLs[crl.certList.Issuer.ToRDNSequence().String()] = crl
		successCounter++
	}
	// Only if all the files are processed successfully we can swap maps (there
	// might be deletions of entries in this case).
	if len(files) == successCounter {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.crls = tempCRLs
		grpclogLogger.Infof("Scan of CRLDirectory %v completed, %v files found and processed successfully, in-memory CRL storage flushed and repopulated", p.opts.CRLDirectory, len(files))
	} else {
		// Since some of the files failed we can only add/update entries in the map.
		p.mu.Lock()
		defer p.mu.Unlock()
		for key, value := range tempCRLs {
			p.crls[key] = value
		}
		grpclogLogger.Infof("Scan of CRLDirectory %v completed, %v files found, %v files processing failed, %v entries of in-memory CRL storage added/updated", p.opts.CRLDirectory, len(files), failCounter, successCounter)
	}
}

// CRL retrieves the CRL associated with the given certificate's issuer DN from
// in-memory if it was loaded during FileWatcherOptions.CRLDirectory scan before
// the execution of this function.
func (p *FileWatcherCRLProvider) CRL(cert *x509.Certificate) (*CRL, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.crls[cert.Issuer.ToRDNSequence().String()], nil
}
