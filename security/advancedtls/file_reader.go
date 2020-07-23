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
	"fmt"
	"io/ioutil"
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

// PemPeerCredFileReader is the default implementation of PeerCredFileReader.
// PemPeerCredFileReader supports reading peer credential files of PEM format.
type PemPeerCredFileReader struct {
	certFilePath string
	keyFilePath  string
}

// PemPeerCredFileReaderOption contains all information needed to be filled out
// by users in order to use NewPemPeerCredFileReader.
type PemPeerCredFileReaderOption struct {
	CertFilePath string
	KeyFilePath  string
}

// NewPemPeerCredFileReader uses PemPeerCredFileReaderOption
// to contruct a PemPeerCredFileReader.
func NewPemPeerCredFileReader(o PemPeerCredFileReaderOption) (*PemPeerCredFileReader, error) {
	if o.CertFilePath == "" {
		return nil, fmt.Errorf("users need to specify certificate file path in PemPeerCredFileReaderOption")
	}
	if o.KeyFilePath == "" {
		return nil, fmt.Errorf("users need to specify key file path in PemPeerCredFileReaderOption")
	}
	r := &PemPeerCredFileReader{
		certFilePath: o.CertFilePath,
		keyFilePath:  o.KeyFilePath,
	}
	return r, nil
}

// ReadKeyAndCerts reads and parses peer certificates from the certificate file path
// and the key file path specified in PemPeerCredFileReader.
func (r PemPeerCredFileReader) ReadKeyAndCerts() (*tls.Certificate, error) {
	cert, err := tls.LoadX509KeyPair(r.certFilePath, r.keyFilePath)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}

// PemTrustCredFileReader is the default implementation of TrustCredFileReader.
// PemTrustCredFileReader supports reading trust credential files of PEM format.
type PemTrustCredFileReader struct {
	trustCertPath string
}

// PemTrustCredFileReaderOption contains all information needed to be filled out
// by users in order to use NewPemTrustCredFileReader.
type PemTrustCredFileReaderOption struct {
	TrustCertPath string
}

// NewPemTrustCredFileReader uses PemTrustCredFileReaderOption
// to contruct a PemTrustCredFileReader.
func NewPemTrustCredFileReader(o PemTrustCredFileReaderOption) (*PemTrustCredFileReader, error) {
	if o.TrustCertPath == "" {
		return nil, fmt.Errorf("users need to specify key file path in PemTrustCredFileReaderOption")
	}
	r := &PemTrustCredFileReader{
		trustCertPath: o.TrustCertPath,
	}
	return r, nil
}

// ReadTrustCerts reads and parses trust certificates from trust certificate path
// specified in PemTrustCredFileReader.
func (r PemTrustCredFileReader) ReadTrustCerts() (*x509.CertPool, error) {
	trustData, err := ioutil.ReadFile(r.trustCertPath)
	if err != nil {
		return nil, err
	}
	trustPool := x509.NewCertPool()
	trustPool.AppendCertsFromPEM(trustData)
	return trustPool, nil
}
