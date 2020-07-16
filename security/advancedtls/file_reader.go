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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"time"
)

// PeerCredFileReader contains
type PeerCredFileReader interface {
	ReadKeyAndCerts() (*tls.Certificate, error)
}

// TrustCredFileReader contains
type TrustCredFileReader interface {
	ReadTrustCerts() (*x509.CertPool, error)
}

type pemPeerCredFileReader struct {
	certFilePath string
	keyFilePath  string
	interval     time.Duration
}

// PemPeerCredFileReaderOption contains
type PemPeerCredFileReaderOption struct {
	CertFilePath string
	KeyFilePath  string
	Interval     time.Duration
}

// NewPemPeerCredFileReader is
func NewPemPeerCredFileReader(o PemPeerCredFileReaderOption) (*pemPeerCredFileReader, error) {
	if o.CertFilePath == "" {
		return nil, fmt.Errorf("PemPeerCredFileReaderOption needs to specify certificate file path")
	}
	if o.KeyFilePath == "" {
		return nil, fmt.Errorf("PemPeerCredFileReaderOption needs to specify key file path")
	}
	// If interval is invalid, set it to 1 hour by default.
	interval := o.Interval
	if interval == 0*time.Hour {
		interval = 1 * time.Hour
	}
	r := &pemPeerCredFileReader{
		certFilePath: o.CertFilePath,
		keyFilePath:  o.KeyFilePath,
		interval:     interval,
	}
	return r, nil
}

func (r pemPeerCredFileReader) ReadKeyAndCerts() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(r.certFilePath, r.keyFilePath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

type pemTrustCredFileReader struct {
	trustCertPath string
	interval      time.Duration
}

// PemTrustCredFileReaderOption contains
type PemTrustCredFileReaderOption struct {
	TrustCertPath string
	Interval      time.Duration
}

// NewPemTrustCredFileReader is
func NewPemTrustCredFileReader(o PemTrustCredFileReaderOption) (*pemTrustCredFileReader, error) {
	if o.TrustCertPath == "" {
		return nil, fmt.Errorf("PemTrustCredFileReaderOption needs to specify certificate file path")
	}
	// If interval is invalid, set it to 1 hour by default.
	interval := o.Interval
	if interval == 0*time.Hour {
		interval = 1 * time.Hour
	}
	r := &pemTrustCredFileReader{
		trustCertPath: o.TrustCertPath,
		interval:      interval,
	}
	return r, nil
}

func (r pemTrustCredFileReader) ReadTrustCerts() (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(r.trustCertPath)
	if err != nil {
		return nil, err
	}
	trustBlock, _ := pem.Decode(trustData)
	if trustBlock == nil {
		return nil, err
	}
	trustCert, err := x509.ParseCertificate(trustBlock.Bytes)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	trustPool.AddCert(trustCert)
	return trustPool, nil
}
