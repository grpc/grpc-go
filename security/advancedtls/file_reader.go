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

// PeerCredFileReader is an interface that supports reading
// peer credential files of different formats.
type PeerCredFileReader interface {
	ReadKeyAndCerts() (*tls.Certificate, error)
}

// TrustCredFileReader is an interface that supports reading
// trust credential files of different formats.
type TrustCredFileReader interface {
	ReadTrustCerts() (*x509.CertPool, error)
}

// pemPeerCredFileReader is the default implementation of PeerCredFileReader.
// pemPeerCredFileReader supports reading peer credential files of PEM format.
type pemPeerCredFileReader struct {
	certFilePath string
	keyFilePath  string
	interval     time.Duration
}

// PemPeerCredFileReaderOption contains all information needed to be filled out
// by users in order to use NewPemPeerCredFileReader.
type PemPeerCredFileReaderOption struct {
	CertFilePath string
	KeyFilePath  string
	Interval     time.Duration
}

// newPemPeerCredFileReader uses PemPeerCredFileReaderOption
// to contruct a pemPeerCredFileReader.
func newPemPeerCredFileReader(o PemPeerCredFileReaderOption) (*pemPeerCredFileReader, error) {
	if o.CertFilePath == "" {
		return nil, fmt.Errorf(
			"users need to specify certificate file path in PemPeerCredFileReaderOption")
	}
	if o.KeyFilePath == "" {
		return nil, fmt.Errorf(
			"users need to specify key file path in PemPeerCredFileReaderOption")
	}
	// If interval is not set by users explicitly, set it to 1 hour by default.
	interval := o.Interval
	if interval == 0*time.Nanosecond {
		interval = 1 * time.Hour
	}
	r := &pemPeerCredFileReader{
		certFilePath: o.CertFilePath,
		keyFilePath:  o.KeyFilePath,
		interval:     interval,
	}
	return r, nil
}

// ReadKeyAndCerts reads and parses peer certificates from the certificate file path
// and the key file path specified in pemPeerCredFileReader.
func (r pemPeerCredFileReader) ReadKeyAndCerts() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(r.certFilePath, r.keyFilePath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// pemTrustCredFileReader is the default implementation of TrustCredFileReader.
// pemTrustCredFileReader supports reading trust credential files of PEM format.
type pemTrustCredFileReader struct {
	trustCertPath string
	interval      time.Duration
}

// PemTrustCredFileReaderOption contains all information needed to be filled out
// by users in order to use NewPemTrustCredFileReader.
type PemTrustCredFileReaderOption struct {
	TrustCertPath string
	Interval      time.Duration
}

// newPemTrustCredFileReader uses PemTrustCredFileReaderOption
// to contruct a pemTrustCredFileReader.
func newPemTrustCredFileReader(o PemTrustCredFileReaderOption) (*pemTrustCredFileReader, error) {
	if o.TrustCertPath == "" {
		return nil, fmt.Errorf(
			"users need to specify key file path in PemTrustCredFileReaderOption")
	}
	// If interval is not set by users explicitly, set it to 1 hour by default.
	interval := o.Interval
	if interval == 0*time.Nanosecond {
		interval = 1 * time.Hour
	}
	r := &pemTrustCredFileReader{
		trustCertPath: o.TrustCertPath,
		interval:      interval,
	}
	return r, nil
}

// ReadTrustCerts reads and parses trust certificates from trust certificate path
// specified in pemTrustCredFileReader.
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
