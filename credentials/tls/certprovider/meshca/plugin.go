// +build go1.13

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

// Package meshca provides an implementation of the Provider interface which
// communicates with MeshCA to get certificates signed.
package meshca

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	durationpb "github.com/golang/protobuf/ptypes/duration"
	"github.com/google/uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider"
	meshgrpc "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	meshpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/metadata"
)

// In requests sent to the MeshCA, we add a metadata header with this key and
// the value being the GCE zone in which the workload is running in.
const locationMetadataKey = "x-goog-request-params"

// For overriding from unit tests.
var newDistributorFunc = func() distributor { return certprovider.NewDistributor() }

// distributor wraps the methods on certprovider.Distributor which are used by
// the plugin. This is very useful in tests which need to know exactly when the
// plugin updates its key material.
type distributor interface {
	KeyMaterial(ctx context.Context) (*certprovider.KeyMaterial, error)
	Set(km *certprovider.KeyMaterial, err error)
	Stop()
}

// providerPlugin is an implementation of the certprovider.Provider interface,
// which gets certificates signed by communicating with the MeshCA.
type providerPlugin struct {
	distributor // Holds the key material.
	cancel      context.CancelFunc
	cc          *grpc.ClientConn          // Connection to MeshCA server.
	cfg         *pluginConfig             // Plugin configuration.
	opts        certprovider.BuildOptions // Key material options.
	logger      *grpclog.PrefixLogger     // Plugin instance specific prefix.
	backoff     func(int) time.Duration   // Exponential backoff.
	doneFunc    func()                    // Notify the builder when done.
}

// providerParams wraps params passed to the provider plugin at creation time.
type providerParams struct {
	// This ClientConn to the MeshCA server is owned by the builder.
	cc       *grpc.ClientConn
	cfg      *pluginConfig
	opts     certprovider.BuildOptions
	backoff  func(int) time.Duration
	doneFunc func()
}

func newProviderPlugin(params providerParams) *providerPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	p := &providerPlugin{
		cancel:      cancel,
		cc:          params.cc,
		cfg:         params.cfg,
		opts:        params.opts,
		backoff:     params.backoff,
		doneFunc:    params.doneFunc,
		distributor: newDistributorFunc(),
	}
	p.logger = prefixLogger((p))
	p.logger.Infof("plugin created")
	go p.run(ctx)
	return p
}

func (p *providerPlugin) Close() {
	p.logger.Infof("plugin closed")
	p.Stop() // Stop the embedded distributor.
	p.cancel()
	p.doneFunc()
}

// run is a long running goroutine which periodically sends out CSRs to the
// MeshCA, and updates the underlying Distributor with the new key material.
func (p *providerPlugin) run(ctx context.Context) {
	// We need to start fetching key material right away. The next attempt will
	// be triggered by the timer firing.
	for {
		certValidity, err := p.updateKeyMaterial(ctx)
		if err != nil {
			return
		}

		// We request a certificate with the configured validity duration (which
		// is usually twice as much as the grace period). But the server is free
		// to return a certificate with whatever validity time it deems right.
		refreshAfter := p.cfg.certGraceTime
		if refreshAfter > certValidity {
			// The default value of cert grace time is half that of the default
			// cert validity time. So here, when we have to use a non-default
			// cert life time, we will set the grace time again to half that of
			// the validity time.
			refreshAfter = certValidity / 2
		}
		timer := time.NewTimer(refreshAfter)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// updateKeyMaterial generates a CSR and attempts to get it signed from the
// MeshCA. It retries with an exponential backoff till it succeeds or the
// deadline specified in ctx expires. Once it gets the CSR signed from the
// MeshCA, it updates the Distributor with the new key material.
//
// It returns the amount of time the new certificate is valid for.
func (p *providerPlugin) updateKeyMaterial(ctx context.Context) (time.Duration, error) {
	client := meshgrpc.NewMeshCertificateServiceClient(p.cc)
	retries := 0
	for {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}

		if retries != 0 {
			bi := p.backoff(retries)
			p.logger.Warningf("Backing off for %s before attempting the next CreateCertificate() request", bi)
			timer := time.NewTimer(bi)
			select {
			case <-timer.C:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		retries++

		privKey, err := rsa.GenerateKey(rand.Reader, p.cfg.keySize)
		if err != nil {
			p.logger.Warningf("RSA key generation failed: %v", err)
			continue
		}
		// We do not set any fields in the CSR (we use an empty
		// x509.CertificateRequest as the template) because the MeshCA discards
		// them anyways, and uses the workload identity from the access token
		// that we present (as part of the STS call creds).
		csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, crypto.PrivateKey(privKey))
		if err != nil {
			p.logger.Warningf("CSR creation failed: %v", err)
			continue
		}
		csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})

		// Send out the CSR with a call timeout and location metadata, as
		// specified in the plugin configuration.
		req := &meshpb.MeshCertificateRequest{
			RequestId: uuid.New().String(),
			Csr:       string(csrPEM),
			Validity:  &durationpb.Duration{Seconds: int64(p.cfg.certLifetime / time.Second)},
		}
		p.logger.Debugf("Sending CreateCertificate() request: %v", req)

		callCtx, ctxCancel := context.WithTimeout(context.Background(), p.cfg.callTimeout)
		callCtx = metadata.NewOutgoingContext(callCtx, metadata.Pairs(locationMetadataKey, p.cfg.location))
		resp, err := client.CreateCertificate(callCtx, req)
		if err != nil {
			p.logger.Warningf("CreateCertificate request failed: %v", err)
			ctxCancel()
			continue
		}
		ctxCancel()

		// The returned cert chain must contain more than one cert. Leaf cert is
		// element '0', while root cert is element 'n', and the intermediate
		// entries form the chain from the root to the leaf.
		certChain := resp.GetCertChain()
		if l := len(certChain); l <= 1 {
			p.logger.Errorf("Received certificate chain contains %d certificates, need more than one", l)
			continue
		}

		// We need to explicitly parse the PEM cert contents as an
		// x509.Certificate to read the certificate validity period. We use this
		// to decide when to refresh the cert. Even though the call to
		// tls.X509KeyPair actually parses the PEM contents into an
		// x509.Certificate, it does not store that in the `Leaf` field. See:
		// https://golang.org/pkg/crypto/tls/#X509KeyPair.
		identity, intermediates, roots, err := parseCertChain(certChain)
		if err != nil {
			p.logger.Errorf(err.Error())
			continue
		}
		_, err = identity.Verify(x509.VerifyOptions{
			Intermediates: intermediates,
			Roots:         roots,
		})
		if err != nil {
			p.logger.Errorf("Certificate verification failed for return certChain: %v", err)
			continue
		}

		key := x509.MarshalPKCS1PrivateKey(privKey)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: key})
		certPair, err := tls.X509KeyPair([]byte(certChain[0]), keyPEM)
		if err != nil {
			p.logger.Errorf("Failed to create x509 key pair: %v", err)
			continue
		}

		// At this point, the received response has been deemed good.
		retries = 0

		// All certs signed by the MeshCA roll up to the same root. And treating
		// the last element of the returned chain as the root is the only
		// supported option to get the root certificate. So, we ignore the
		// options specified in the call to Build(), which contain certificate
		// name and whether the caller is interested in identity or root cert.
		p.Set(&certprovider.KeyMaterial{Certs: []tls.Certificate{certPair}, Roots: roots}, nil)
		return time.Until(identity.NotAfter), nil
	}
}

// ParseCertChain parses the result returned by the MeshCA which consists of a
// list of PEM encoded certs. The first element in the list is the leaf or
// identity cert, while the last element is the root, and everything in between
// form the chain of trust.
//
// Caller needs to make sure that certChain has at least two elements.
func parseCertChain(certChain []string) (*x509.Certificate, *x509.CertPool, *x509.CertPool, error) {
	identity, err := parseCert([]byte(certChain[0]))
	if err != nil {
		return nil, nil, nil, err
	}

	intermediates := x509.NewCertPool()
	for _, cert := range certChain[1 : len(certChain)-1] {
		i, err := parseCert([]byte(cert))
		if err != nil {
			return nil, nil, nil, err
		}
		intermediates.AddCert(i)
	}

	roots := x509.NewCertPool()
	root, err := parseCert([]byte(certChain[len(certChain)-1]))
	if err != nil {
		return nil, nil, nil, err
	}
	roots.AddCert(root)

	return identity, intermediates, roots, nil
}

func parseCert(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode received PEM data: %v", certPEM)
	}
	return x509.ParseCertificate(block.Bytes)
}
