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

package certprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/testdata"
)

const (
	fakeProvider1Name  = "fake-certificate-provider-1"
	fakeProvider2Name  = "fake-certificate-provider-2"
	fakeConfig         = "my fake config"
	providerChanSize   = 2
	defaultTestTimeout = 1 * time.Second
)

var fpb1, fpb2 *fakeProviderBuilder

func init() {
	fpb1 = &fakeProviderBuilder{
		name:         fakeProvider1Name,
		providerChan: testutils.NewChannelWithSize(providerChanSize),
	}
	fpb2 = &fakeProviderBuilder{
		name:         fakeProvider2Name,
		providerChan: testutils.NewChannelWithSize(providerChanSize),
	}
	Register(fpb1)
	Register(fpb2)
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

// fakeProviderBuilder builds new instances of fakeProvider and interprets the
// config provided to it as a string.
type fakeProviderBuilder struct {
	name         string
	providerChan *testutils.Channel
}

func (b *fakeProviderBuilder) Build(StableConfig) Provider {
	p := &fakeProvider{distributors: make(map[KeyMaterialOptions]*Distributor)}
	b.providerChan.Send(p)
	return p
}

func (b *fakeProviderBuilder) ParseConfig(config interface{}) (StableConfig, error) {
	s, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("providerBuilder %s received config of type %T, want string", b.name, config)
	}
	return &fakeStableConfig{config: s}, nil
}

func (b *fakeProviderBuilder) Name() string {
	return b.name
}

type fakeStableConfig struct {
	config string
}

func (c *fakeStableConfig) Canonical() []byte {
	return []byte(c.config)
}

// fakeProvider is an implementation of the Provider interface which provides a
// method for tests to invoke to push new key materials.
type fakeProvider struct {
	mu           sync.Mutex
	distributors map[KeyMaterialOptions]*Distributor
}

// newKeyMaterial allows tests to push new key material to the fake provider
// which will be made available to users of this provider.
func (p *fakeProvider) newKeyMaterial(opts KeyMaterialOptions, km *KeyMaterial, err error) {
	p.mu.Lock()
	dist, ok := p.distributors[opts]
	if !ok {
		dist = NewDistributor()
		p.distributors[opts] = dist
	}
	dist.Set(km, err)
	p.mu.Unlock()
}

// KeyMaterialReader helps implement the Provider interface.
func (p *fakeProvider) KeyMaterialReader(opts KeyMaterialOptions) (KeyMaterialReader, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	dist, ok := p.distributors[opts]
	if !ok {
		dist = NewDistributor()
		p.distributors[opts] = dist
	}
	return dist, nil
}

// Close helps implement the Provider interface.
func (p *fakeProvider) Close() {
	p.mu.Lock()
	for _, d := range p.distributors {
		d.Close()
	}
	p.mu.Unlock()
}

// loadKeyMaterials is a helper to read cert/key files from testdata and convert
// them into a KeyMaterialReader struct.
func loadKeyMaterials(t *testing.T, cert, key, ca string) *KeyMaterial {
	t.Helper()

	certs, err := tls.LoadX509KeyPair(testdata.Path(cert), testdata.Path(key))
	if err != nil {
		t.Fatalf("Failed to load keyPair: %v", err)
	}

	pemData, err := ioutil.ReadFile(testdata.Path(ca))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pemData)
	return &KeyMaterial{Certs: []tls.Certificate{certs}, Roots: roots}
}

// readAndVerifyKeyMaterial attempts to read key material from the provider
// reader and compares it against the expected key material.
func readAndVerifyKeyMaterial(kmr KeyMaterialReader, wantKM *KeyMaterial) error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	gotKM, err := kmr.KeyMaterial(ctx)
	if err != nil {
		return fmt.Errorf("KeyMaterial(ctx) failed: %w", err)
	}
	return compareKeyMaterial(gotKM, wantKM)
}

func compareKeyMaterial(got, want *KeyMaterial) error {
	// TODO(easwars): Remove all references to reflect.DeepEqual and use
	// cmp.Equal instead. Currently, the later panics because x509.Certificate
	// type defines an Equal method, but does not check for nil. This has been
	// fixed in
	// https://github.com/golang/go/commit/89865f8ba64ccb27f439cce6daaa37c9aa38f351,
	// but this is only available starting go1.14. So, once we remove support
	// for go1.13, we can make the switch.
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("provider.KeyMaterial() = %+v, want %+v", got, want)
	}
	return nil
}

// TestStoreSingleProviderSingleReader creates a single provider and single
// reader through the store and calls methods on them.
func (s) TestStoreSingleProviderSingleReader(t *testing.T) {
	// Create a KeyMaterialReader through the store. This will internally create
	// a provider and a distributor.
	kmOpts := KeyMaterialOptions{CertName: "default"}
	kmReader, err := GetKeyMaterialReader(fakeProvider1Name, fakeConfig, kmOpts)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, kmOpts, err)
	}
	defer kmReader.Close()

	// Our fakeProviderBuilder pushes newly created providers on a channel. Grab
	// the fake provider from that channel.
	p, err := fpb1.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider1Name)
	}
	prov := p.(*fakeProvider)

	// Attempt to read from key material from the reader returned by the store.
	// This will fail because we have not pushed any key material into our fake
	// provider.
	if err := readAndVerifyKeyMaterial(kmReader, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}

	// Load key material from testdata directory, push it into out fakeProvider
	// and attempt to read from the reader returned by the store.
	testKM1 := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	prov.newKeyMaterial(kmOpts, testKM1, nil)
	if err := readAndVerifyKeyMaterial(kmReader, testKM1); err != nil {
		t.Fatal(err)
	}

	// Push new key material and read from the reader. This should returned
	// updated key material.
	testKM2 := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")
	prov.newKeyMaterial(kmOpts, testKM2, nil)
	if err := readAndVerifyKeyMaterial(kmReader, testKM2); err != nil {
		t.Fatal(err)
	}
}

// TestStoreSingleProviderMultipleReaders creates key material readers on a
// single provider through the store (and expects the store's sharing mechanism
// to kick in) and calls methods on them.
func (s) TestStoreSingleProviderMultipleReaders(t *testing.T) {
	// Create three readers on the same fake provider. Two of these readers use
	// certName `foo`, while the third one uses certName `bar`.
	optsFoo := KeyMaterialOptions{CertName: "foo"}
	optsBar := KeyMaterialOptions{CertName: "bar"}
	readerFoo1, err := GetKeyMaterialReader(fakeProvider1Name, fakeConfig, optsFoo)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, optsFoo, err)
	}
	defer readerFoo1.Close()
	readerFoo2, err := GetKeyMaterialReader(fakeProvider1Name, fakeConfig, optsFoo)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, optsFoo, err)
	}
	defer readerFoo2.Close()
	readerBar1, err := GetKeyMaterialReader(fakeProvider1Name, fakeConfig, optsBar)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, optsBar, err)
	}
	defer readerBar1.Close()

	// Our fakeProviderBuilder pushes newly created providers on a channel. Grab
	// the fake provider from that channel.
	p, err := fpb1.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider1Name)
	}
	prov := p.(*fakeProvider)

	// Make sure the store does not create multiple providers here.
	if _, err := fpb1.providerChan.Receive(); err != testutils.ErrRecvTimeout {
		t.Fatal("Multiple certProviders created when only one was expected")
	}

	// Push key material for optsFoo, and make sure the foo readers return
	// appropriate key material and the bar reader times out.
	fooKM := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	prov.newKeyMaterial(optsFoo, fooKM, nil)
	if err := readAndVerifyKeyMaterial(readerFoo1, fooKM); err != nil {
		t.Fatal(err)
	}
	if err := readAndVerifyKeyMaterial(readerFoo2, fooKM); err != nil {
		t.Fatal(err)
	}
	if err := readAndVerifyKeyMaterial(readerBar1, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}

	// Push key material for optsBar, and make sure the bar reader returns appropriate key material.
	barKM := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")
	prov.newKeyMaterial(optsBar, barKM, nil)
	if err := readAndVerifyKeyMaterial(readerBar1, barKM); err != nil {
		t.Fatal(err)
	}

	// Make sure the above push of new key material does not affect foo readers.
	if err := readAndVerifyKeyMaterial(readerFoo1, fooKM); err != nil {
		t.Fatal(err)
	}
}

// TestStoreMultipleProviderInstancesOfSameType creates multiple instances of
// the same type of provider through the store with different configs. The store
// would end up creating different provider instances for these and no sharing
// would take place.
func (s) TestStoreMultipleProviderInstancesOfSameType(t *testing.T) {
	// Create three readers on the same fake provider. Two of these readers use
	// certName `foo`, while the third one uses certName `bar`.
	opts := KeyMaterialOptions{CertName: "foo"}
	cfg1 := fakeConfig + "1111"
	cfg2 := fakeConfig + "2222"
	reader1, err := GetKeyMaterialReader(fakeProvider1Name, cfg1, opts)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, cfg1, opts, err)
	}
	defer reader1.Close()
	reader2, err := GetKeyMaterialReader(fakeProvider1Name, cfg2, opts)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, cfg2, opts, err)
	}
	defer reader2.Close()

	// Our fakeProviderBuilder pushes newly created providers on a channel. Grab
	// the fake provider from that channel.
	p1, err := fpb1.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider1Name)
	}
	prov1 := p1.(*fakeProvider)

	p2, err := fpb1.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider1Name)
	}
	prov2 := p2.(*fakeProvider)

	// Push the same key material into both providers and verify that the
	// readers return the appropriate key material.
	km1 := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	prov1.newKeyMaterial(opts, km1, nil)
	prov2.newKeyMaterial(opts, km1, nil)
	if err := readAndVerifyKeyMaterial(reader1, km1); err != nil {
		t.Fatal(err)
	}
	if err := readAndVerifyKeyMaterial(reader2, km1); err != nil {
		t.Fatal(err)
	}

	// Push new key material into only one of the providers and and verify that
	// the readers return the appropriate key material.
	km2 := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")
	prov2.newKeyMaterial(opts, km2, nil)
	if err := readAndVerifyKeyMaterial(reader1, km1); err != nil {
		t.Fatal(err)
	}
	if err := readAndVerifyKeyMaterial(reader2, km2); err != nil {
		t.Fatal(err)
	}

	// Close one of the readers and verify that the other one is not affected.
	reader1.Close()
	if err := readAndVerifyKeyMaterial(reader2, km2); err != nil {
		t.Fatal(err)
	}
}

// TestStoreMultipleProviders creates providers of different types and makes
// sure closing of one does not affect the other.
func (s) TestStoreMultipleProviders(t *testing.T) {
	opts := KeyMaterialOptions{CertName: "foo"}
	reader1, err := GetKeyMaterialReader(fakeProvider1Name, fakeConfig, opts)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, opts, err)
	}
	defer reader1.Close()
	reader2, err := GetKeyMaterialReader(fakeProvider2Name, fakeConfig, opts)
	if err != nil {
		t.Fatalf("GetKeyMaterialReader(%s, %s, %v) failed: %v", fakeProvider1Name, fakeConfig, opts, err)
	}
	defer reader2.Close()

	// Our fakeProviderBuilder pushes newly created providers on a channel. Grab
	// the fake provider from that channel.
	p1, err := fpb1.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider1Name)
	}
	prov1 := p1.(*fakeProvider)

	p2, err := fpb2.providerChan.Receive()
	if err != nil {
		t.Fatalf("Timeout when expecting certProvider %q to be created", fakeProvider2Name)
	}
	prov2 := p2.(*fakeProvider)

	// Push the key material into both providers and verify that the
	// readers return the appropriate key material.
	km1 := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	prov1.newKeyMaterial(opts, km1, nil)
	km2 := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")
	prov2.newKeyMaterial(opts, km2, nil)
	if err := readAndVerifyKeyMaterial(reader1, km1); err != nil {
		t.Fatal(err)
	}
	if err := readAndVerifyKeyMaterial(reader2, km2); err != nil {
		t.Fatal(err)
	}

	// Close one of the readers and verify that the other one is not affected.
	reader1.Close()
	if err := readAndVerifyKeyMaterial(reader2, km2); err != nil {
		t.Fatal(err)
	}
}
