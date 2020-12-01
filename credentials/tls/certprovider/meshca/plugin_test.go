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
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/tls/certprovider"
	meshgrpc "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	meshpb "google.golang.org/grpc/credentials/tls/certprovider/meshca/internal/v1"
	"google.golang.org/grpc/internal/testutils"
)

const (
	// Used when waiting for something that is expected to *not* happen.
	defaultTestShortTimeout = 10 * time.Millisecond
	defaultTestTimeout      = 5 * time.Second
	defaultTestCertLife     = time.Hour
	shortTestCertLife       = 2 * time.Second
	maxErrCount             = 2
)

// fakeCA provides a very simple fake implementation of the certificate signing
// service as exported by the MeshCA.
type fakeCA struct {
	meshgrpc.UnimplementedMeshCertificateServiceServer

	withErrors    bool // Whether the CA returns errors to begin with.
	withShortLife bool // Whether to create certs with short lifetime

	ccChan  *testutils.Channel // Channel to get notified about CreateCertificate calls.
	errors  int                // Error count.
	key     *rsa.PrivateKey    // Private key of CA.
	cert    *x509.Certificate  // Signing certificate.
	certPEM []byte             // PEM encoding of signing certificate.
}

// Returns a new instance of the fake Mesh CA. It generates a new RSA key and a
// self-signed certificate which will be used to sign CSRs received in incoming
// requests.
// withErrors controls whether the fake returns errors before succeeding, while
// withShortLife controls whether the fake returns certs with very small
// lifetimes (to test plugin refresh behavior). Every time a CreateCertificate()
// call succeeds, an event is pushed on the ccChan.
func newFakeMeshCA(ccChan *testutils.Channel, withErrors, withShortLife bool) (*fakeCA, error) {
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
		ccChan:        ccChan,
		key:           key,
		cert:          cert,
		certPEM:       certPEM,
	}, nil
}

// CreateCertificate helps implement the MeshCA service.
//
// If the fakeMeshCA was created with `withErrors` set to true, the first
// `maxErrCount` number of RPC return errors. Subsequent requests are signed and
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

	// Push to ccChan to indicate that the RPC is processed.
	f.ccChan.Send(nil)

	certChain := []string{
		string(certPEM),   // Signed certificate corresponding to CSR
		string(f.certPEM), // Root certificate
	}
	return &meshpb.MeshCertificateResponse{CertChain: certChain}, nil
}

// opts wraps the options to be passed to setup.
type opts struct {
	// Whether the CA returns certs with short lifetime. Used to test client refresh.
	withShortLife bool
	// Whether the CA returns errors to begin with. Used to test client backoff.
	withbackoff bool
}

// events wraps channels which indicate different events.
type events struct {
	// Pushed to when the plugin dials the MeshCA.
	dialDone *testutils.Channel
	// Pushed to when CreateCertifcate() succeeds on the MeshCA.
	createCertDone *testutils.Channel
	// Pushed to when the plugin updates the distributor with new key material.
	keyMaterialDone *testutils.Channel
	// Pushed to when the client backs off after a failed CreateCertificate().
	backoffDone *testutils.Channel
}

// setup performs tasks common to all tests in this file.
func setup(t *testing.T, o opts) (events, string, func()) {
	t.Helper()

	// Create a fake MeshCA which pushes events on the passed channel for
	// successful RPCs.
	createCertDone := testutils.NewChannel()
	fs, err := newFakeMeshCA(createCertDone, o.withbackoff, o.withShortLife)
	if err != nil {
		t.Fatal(err)
	}

	// Create a gRPC server and register the fake MeshCA on it.
	server := grpc.NewServer()
	meshgrpc.RegisterMeshCertificateServiceServer(server, fs)

	// Start a net.Listener on a local port, and pass it to the gRPC server
	// created above and start serving.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	go server.Serve(lis)

	// Override the plugin's dial function and perform a blocking dial. Also
	// push on dialDone once the dial is complete so that test can block on this
	// event before verifying other things.
	dialDone := testutils.NewChannel()
	origDialFunc := grpcDialFunc
	grpcDialFunc = func(uri string, _ ...grpc.DialOption) (*grpc.ClientConn, error) {
		if uri != addr {
			t.Fatalf("plugin dialing MeshCA at %s, want %s", uri, addr)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		cc, err := grpc.DialContext(ctx, uri, grpc.WithInsecure(), grpc.WithBlock())
		if err != nil {
			t.Fatalf("grpc.DialContext(%s) failed: %v", addr, err)
		}
		dialDone.Send(nil)
		return cc, nil
	}

	// Override the plugin's newDistributorFunc and return a wrappedDistributor
	// which allows the test to be notified whenever the plugin pushes new key
	// material into the distributor.
	origDistributorFunc := newDistributorFunc
	keyMaterialDone := testutils.NewChannel()
	d := newWrappedDistributor(keyMaterialDone)
	newDistributorFunc = func() distributor { return d }

	// Override the plugin's backoff function to perform no real backoff, but
	// push on a channel so that the test can verifiy that backoff actually
	// happened.
	backoffDone := testutils.NewChannelWithSize(maxErrCount)
	origBackoffFunc := backoffFunc
	if o.withbackoff {
		// Override the plugin's backoff function with this, so that we can verify
		// that a backoff actually was triggered.
		backoffFunc = func(v int) time.Duration {
			backoffDone.Send(v)
			return 0
		}
	}

	// Return all the channels, and a cancel function to undo all the overrides.
	e := events{
		dialDone:        dialDone,
		createCertDone:  createCertDone,
		keyMaterialDone: keyMaterialDone,
		backoffDone:     backoffDone,
	}
	done := func() {
		server.Stop()
		grpcDialFunc = origDialFunc
		newDistributorFunc = origDistributorFunc
		backoffFunc = origBackoffFunc
	}
	return e, addr, done
}

// wrappedDistributor wraps a distributor and pushes on a channel whenever new
// key material is pushed to the distributor.
type wrappedDistributor struct {
	*certprovider.Distributor
	kmChan *testutils.Channel
}

func newWrappedDistributor(kmChan *testutils.Channel) *wrappedDistributor {
	return &wrappedDistributor{
		kmChan:      kmChan,
		Distributor: certprovider.NewDistributor(),
	}
}

func (wd *wrappedDistributor) Set(km *certprovider.KeyMaterial, err error) {
	wd.Distributor.Set(km, err)
	wd.kmChan.Send(nil)
}

// TestCreateCertificate verifies the simple case where the MeshCA server
// returns a good certificate.
func (s) TestCreateCertificate(t *testing.T) {
	e, addr, cancel := setup(t, opts{})
	defer cancel()

	// Set the MeshCA targetURI to point to our fake MeshCA.
	inputConfig := json.RawMessage(fmt.Sprintf(goodConfigFormatStr, addr))

	// Lookup MeshCA plugin builder, parse config and start the plugin.
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.BuildOptions{})
	if err != nil {
		t.Fatalf("GetProvider(%s, %s) failed: %v", pluginName, string(inputConfig), err)
	}
	defer prov.Close()

	// Wait till the plugin dials the MeshCA server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := e.dialDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to dial MeshCA")
	}

	// Wait till the plugin makes a CreateCertificate() call.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := e.createCertDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to make CreateCertificate RPC")
	}

	// We don't really care about the exact key material returned here. All we
	// care about is whether we get any key material at all, and that we don't
	// get any errors.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = prov.KeyMaterial(ctx); err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}
}

// TestCreateCertificateWithBackoff verifies the case where the MeshCA server
// returns errors initially and then returns a good certificate. The test makes
// sure that the client backs off when the server returns errors.
func (s) TestCreateCertificateWithBackoff(t *testing.T) {
	e, addr, cancel := setup(t, opts{withbackoff: true})
	defer cancel()

	// Set the MeshCA targetURI to point to our fake MeshCA.
	inputConfig := json.RawMessage(fmt.Sprintf(goodConfigFormatStr, addr))

	// Lookup MeshCA plugin builder, parse config and start the plugin.
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.BuildOptions{})
	if err != nil {
		t.Fatalf("GetProvider(%s, %s) failed: %v", pluginName, string(inputConfig), err)
	}
	defer prov.Close()

	// Wait till the plugin dials the MeshCA server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := e.dialDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to dial MeshCA")
	}

	// Making the CreateCertificateRPC involves generating the keys, creating
	// the CSR etc which seem to take reasonable amount of time. And in this
	// test, the first two attempts will fail. Hence we give it a reasonable
	// deadline here.
	ctx, cancel = context.WithTimeout(context.Background(), 3*defaultTestTimeout)
	defer cancel()
	if _, err := e.createCertDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to make CreateCertificate RPC")
	}

	// The first `maxErrCount` calls to CreateCertificate end in failure, and
	// should lead to a backoff.
	for i := 0; i < maxErrCount; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		if _, err := e.backoffDone.Receive(ctx); err != nil {
			t.Fatalf("plugin failed to backoff after error from fake server: %v", err)
		}
	}

	// We don't really care about the exact key material returned here. All we
	// care about is whether we get any key material at all, and that we don't
	// get any errors.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err = prov.KeyMaterial(ctx); err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}
}

// TestCreateCertificateWithRefresh verifies the case where the MeshCA returns a
// certificate with a really short lifetime, and makes sure that the plugin
// refreshes the cert in time.
func (s) TestCreateCertificateWithRefresh(t *testing.T) {
	e, addr, cancel := setup(t, opts{withShortLife: true})
	defer cancel()

	// Set the MeshCA targetURI to point to our fake MeshCA.
	inputConfig := json.RawMessage(fmt.Sprintf(goodConfigFormatStr, addr))

	// Lookup MeshCA plugin builder, parse config and start the plugin.
	prov, err := certprovider.GetProvider(pluginName, inputConfig, certprovider.BuildOptions{})
	if err != nil {
		t.Fatalf("GetProvider(%s, %s) failed: %v", pluginName, string(inputConfig), err)
	}
	defer prov.Close()

	// Wait till the plugin dials the MeshCA server.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := e.dialDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to dial MeshCA")
	}

	// Wait till the plugin makes a CreateCertificate() call.
	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := e.createCertDone.Receive(ctx); err != nil {
		t.Fatal("timeout waiting for plugin to make CreateCertificate RPC")
	}

	ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	km1, err := prov.KeyMaterial(ctx)
	if err != nil {
		t.Fatalf("provider.KeyMaterial(ctx) failed: %v", err)
	}

	// At this point, we have read the first key material, and since the
	// returned key material has a really short validity period, we expect the
	// key material to be refreshed quite soon. We drain the channel on which
	// the event corresponding to setting of new key material is pushed. This
	// enables us to block on the same channel, waiting for refreshed key
	// material.
	// Since we do not expect this call to block, it is OK to pass the
	// background context.
	e.keyMaterialDone.Receive(context.Background())

	// Wait for the next call to CreateCertificate() to refresh the certificate
	// returned earlier.
	ctx, cancel = context.WithTimeout(context.Background(), 2*shortTestCertLife)
	defer cancel()
	if _, err := e.keyMaterialDone.Receive(ctx); err != nil {
		t.Fatalf("CreateCertificate() RPC not made: %v", err)
	}

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
