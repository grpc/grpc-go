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

package meshca

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider"
	configpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/meshca_experimental"
	meshgrpc "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	meshpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	"google.golang.org/grpc/internal/testutils"
)

const (
	defaultTestCertLife = time.Hour
	shortTestCertLife   = 2 * time.Second
	maxErrCount         = 2
)

// fakeCA provides a very simple fake implementation of the certificate signing
// service as exported by the MeshCA.
type fakeCA struct {
	withErrors    bool // Whether the CA returns errors to begin with.
	withShortLife bool // Whether to create certs with short lifetime

	errors  int               // Error count.
	key     *rsa.PrivateKey   // Private key of CA.
	cert    *x509.Certificate // Signing certificate.
	certPEM []byte            // PEM encoding of signing certificate.
}

// Returns a new instance of the fake Mesh CA. It generates a new RSA key and a
// self-signed certificate which will be used to sign CSRs received in incoming
// requests.
// withErrors controls whether the fake returns errors before succeeding, while
// withShortLife controls whether the fake returns certs with very small
// lifetimes (to test plugin refresh behavior).
func newFakeMeshCA(withErrors, withShortLife bool) (*fakeCA, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("RSA key generation failed: %v", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "my-fake-ca"},
		SerialNumber:          big.NewInt(10),
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("x509.CreateCertificate(%v) failed: %v", tmpl, err)
	}
	// The PEM encoding of the self-signed certificate is stored because we need
	// to return a chain of certificates in the response, starting with the
	// client certificate and ending in the root.
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("x509.ParseCertificate(%v) failed: %v", certDER, err)
	}

	return &fakeCA{
		withErrors:    withErrors,
		withShortLife: withShortLife,
		key:           key,
		cert:          cert,
		certPEM:       certPEM,
	}, nil
}

// CreateCertificate helps implement the MeshCA service.
//
// If the fakeMeshCA was created with `withErrors` set to true, the first
// `maxErrCount` number of RPC return errors. Subsequent requests and signed and
// returned without error.
func (f *fakeCA) CreateCertificate(ctx context.Context, req *meshpb.MeshCertificateRequest) (*meshpb.MeshCertificateResponse, error) {
	if f.withErrors {
		if f.errors < maxErrCount {
			f.errors++
			return nil, errors.New("fake Mesh CA error")

		}
	}

	csrPEM := []byte(req.GetCsr())
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode received CSR: %v", csrPEM)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse received CSR: %v", csrPEM)
	}

	// By default, we create certs which are valid for an hour. But if
	// `withShortLife` is set, we create certs which are valid only for a couple
	// of seconds.
	now := time.Now()
	notBefore, notAfter := now.Add(-defaultTestCertLife), now.Add(defaultTestCertLife)
	if f.withShortLife {
		notBefore, notAfter = now.Add(-shortTestCertLife), now.Add(shortTestCertLife)
	}
	tmpl := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "signed-cert"},
		SerialNumber: big.NewInt(10),
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, f.cert, csr.PublicKey, f.key)
	if err != nil {
		return nil, fmt.Errorf("x509.CreateCertificate(%v) failed: %v", tmpl, err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	certChain := []string{
		string(certPEM),   // Signed certificate corresponding to CSR
		string(f.certPEM), // Root certificate
	}
	return &meshpb.MeshCertificateResponse{CertChain: certChain}, nil
}

// setupFakeMeshCA creates a new instance of the fake MeshCA, starts a gRPC
// server on a local port and exports the fake Mesh CA service through the gRPC
// server. It returns the address on which the fake service accepts requests,
// and a cancel function to be invoked when the test is done.
func setupFakeMeshCA(t *testing.T, withErrors, withShortLife bool) (addr string, cancel func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	fs, err := newFakeMeshCA(withErrors, withShortLife)
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	meshgrpc.RegisterMeshCertificateServiceService(server, &meshgrpc.MeshCertificateServiceService{
		CreateCertificate: fs.CreateCertificate,
	})
	go server.Serve(lis)
	return lis.Addr().String(), func() { server.Stop() }
}

// TestCreateCertificate spawns up a fake Mesh CA server, and creates a plugin
// and attempts to read key material from it.
func (s) TestCreateCertificate(t *testing.T) {
	addr, cancel := setupFakeMeshCA(t, false, false)
	defer cancel()

	// Override the dial func to make sure that the plugin is dialing the
	// expected fakeCA, and also to use insecure creds here.
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(uri string, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
		if uri != addr {
			t.Fatalf("plugin dialing MeshCA at %s, want %s", uri, addr)
		}
		return grpc.Dial(uri, grpc.WithInsecure())
	}
	defer func() { grpcDialFunc = origDialFunc }()

	// Set the MeshCA targetURI in the plugin configuration to point to our fake
	// MeshCA.
	cfg := proto.Clone(goodConfigFullySpecified).(*configpb.GoogleMeshCaConfig)
	cfg.Server.GrpcServices[0].GetGoogleGrpc().TargetUri = addr
	inputConfig := makeJSONConfig(t, cfg)
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.Options{})
	if err != nil {
		t.Fatalf("certprovider.GetProvider(%s, %s) failed: %v", pluginName, cfg, err)
	}
	defer prov.Close()

	// We don't really care about the exact key material returned here. All we
	// care about is whether we get any key material at all, and that we don't
	// get any errors.
	// We give the context a larger deadline than usual because the CSR
	// generation seems to be taking quite a bit of time, especially under the
	// race detector.
	ctx, cancel := context.WithTimeout(context.Background(), 3*defaultTestTimeout)
	defer cancel()
	if _, err = prov.KeyMaterial(ctx); err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}
}

func (s) TestCreateCertificateWithBackoff(t *testing.T) {
	addr, cancel := setupFakeMeshCA(t, true, false)
	defer cancel()

	// Override the dial func to make sure that the plugin is dialing the
	// expected fakeCA, and also to use insecure creds here.
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(uri string, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
		if uri != addr {
			t.Fatalf("plugin dialing MeshCA at %s, want %s", uri, addr)
		}
		return grpc.Dial(uri, grpc.WithInsecure())
	}
	defer func() { grpcDialFunc = origDialFunc }()

	// Override the plugin's backoff function with this, so that we can verify
	// that a backoff actually was triggered.
	origBackoffFunc := backoffFunc
	boCh := testutils.NewChannelWithSize(maxErrCount)
	backoffFunc = func(v int) time.Duration {
		boCh.Send(v)
		return 0
	}
	defer func() { backoffFunc = origBackoffFunc }()

	// Set the MeshCA targetURI in the plugin configuration to point to our fake
	// MeshCA.
	cfg := proto.Clone(goodConfigFullySpecified).(*configpb.GoogleMeshCaConfig)
	cfg.Server.GrpcServices[0].GetGoogleGrpc().TargetUri = addr
	inputConfig := makeJSONConfig(t, cfg)
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.Options{})
	if err != nil {
		t.Fatalf("certprovider.GetProvider(%s, %s) failed: %v", pluginName, cfg, err)
	}
	defer prov.Close()

	// We don't really care about the exact key material returned here. All we
	// care about is whether we get any key material at all, and that we don't
	// get any errors.
	ctx, cancel := context.WithTimeout(context.Background(), 3*defaultTestTimeout)
	defer cancel()
	if _, err = prov.KeyMaterial(ctx); err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < maxErrCount; i++ {
		if _, err := boCh.Receive(ctx); err != nil {
			t.Fatalf("plugin failed to backoff after error from fake server: %v", err)
		}
	}
}

func (s) TestCreateCertificateWithRefresh(t *testing.T) {
	addr, cancel := setupFakeMeshCA(t, false, true)
	defer cancel()

	// Override the dial func to make sure that the plugin is dialing the
	// expected fakeCA, and also to use insecure creds here.
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(uri string, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
		if uri != addr {
			t.Fatalf("plugin dialing MeshCA at %s, want %s", uri, addr)
		}
		return grpc.Dial(uri, grpc.WithInsecure())
	}
	defer func() { grpcDialFunc = origDialFunc }()

	// Set the MeshCA targetURI in the plugin configuration to point to our fake
	// MeshCA.
	cfg := proto.Clone(goodConfigFullySpecified).(*configpb.GoogleMeshCaConfig)
	cfg.Server.GrpcServices[0].GetGoogleGrpc().TargetUri = addr
	inputConfig := makeJSONConfig(t, cfg)
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.Options{})
	if err != nil {
		t.Fatalf("certprovider.GetProvider(%s, %s) failed: %v", pluginName, cfg, err)
	}
	defer prov.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*defaultTestTimeout)
	defer cancel()
	km1, err := prov.KeyMaterial(ctx)
	if err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}

	// Sleep for a little while to make sure that the certificate refresh
	// happens in the background.
	time.Sleep(2 * shortTestCertLife)

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	km2, err := prov.KeyMaterial(ctx)
	if err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}

	// TODO(easwars): Remove all references to reflect.DeepEqual and use
	// cmp.Equal instead. Currently, the later panics because x509.Certificate
	// type defines an Equal method, but does not check for nil. This has been
	// fixed in
	// https://github.com/golang/go/commit/89865f8ba64ccb27f439cce6daaa37c9aa38f351,
	// but this is only available starting go1.14. So, once we remove support
	// for go1.13, we can make the switch.
	if reflect.DeepEqual(km1, km2) {
		t.Error("certificate refresh did not happen in the background")
	}
}
