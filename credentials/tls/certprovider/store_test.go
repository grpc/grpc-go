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
	"math/big"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/testdata"
)

const (
	fakeProvider1Name  = "fake-certificate-provider-1"
	fakeProvider2Name  = "fake-certificate-provider-2"
	fakeConfig         = "my fake config"
	defaultTestTimeout = 1 * time.Second
)

func init() {
	Register(&fakeProviderBuilder{name: fakeProvider1Name})
	Register(&fakeProviderBuilder{name: fakeProvider2Name})
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
	name string
}

func (b *fakeProviderBuilder) Build(StableConfig) Provider {
	p := &fakeProvider{
		Distributor: NewDistributor(),
		done:        make(chan struct{}),
		kmCh:        make(chan *KeyMaterial, 2),
	}
	go p.run()
	return p
}

func (b *fakeProviderBuilder) ParseConfig(config interface{}) (StableConfig, error) {
	s, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("provider %s received bad config %v", b.name, config)
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

// fakeProvider is an implementation of the Provider interface which embeds a
// Distributor and exposes two channels for the user:
// 1. to be notified when the provider is closed
// 2. to push new key material into the provider
type fakeProvider struct {
	*Distributor

	// This channel is closed when the provider is closed. Tests should block on
	// this to make sure the provider is closed.
	done chan struct{}
	// Tests can push new key material on this channel, and the provider will
	// return this on subsequent calls to KeyMaterial().
	kmCh chan *KeyMaterial
}

func (p *fakeProvider) run() {
	for {
		select {
		case <-p.Distributor.Done():
			return
		case km := <-p.kmCh:
			p.Distributor.Set(km)
		}
	}
}

// loadKeyMaterials is a helper to read cert/key files from testdata and convert
// them into a KeyMaterial struct.
func loadKeyMaterials() (*KeyMaterial, error) {
	certs, err := tls.LoadX509KeyPair(testdata.Path("server1.pem"), testdata.Path("server1.key"))
	if err != nil {
		return nil, err
	}

	pemData, err := ioutil.ReadFile(testdata.Path("ca.pem"))
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(pemData)
	return &KeyMaterial{Certs: []tls.Certificate{certs}, Roots: roots}, nil
}

func makeProvider(t *testing.T, name, config string) (Provider, *fakeProvider) {
	t.Helper()

	// Grab the provider builder.
	b := Get(name)
	if b == nil {
		t.Fatalf("no provider builder found for name : %s", name)
	}

	// Ask the builder to parse the config.
	cfg, err := b.ParseConfig(config)
	if err != nil {
		t.Fatalf("%s.ParseConfig(%v) = %v", name, config, err)
	}

	// Pass the config to the store to fetch a provider.
	store := GetStore()
	key := Key{Name: name, Config: cfg}
	prov := store.GetProvider(key)
	if prov == nil {
		t.Fatalf("store.Get(%v) is nil", key)
	}

	// The store returns a wrappedProvider, which holds a reference to the
	// actual provider, which in our case in the fakeProvider.
	wp := prov.(*wrappedProvider)
	fp := wp.Provider.(*fakeProvider)
	return prov, fp
}

// TestStoreWithSingleProvider creates a single provider through the store and
// calls methods on it.
func (s) TestStoreWithSingleProvider(t *testing.T) {
	prov, fp := makeProvider(t, fakeProvider1Name, fakeConfig)

	// Push key materials into the provider.
	wantKM, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	fp.kmCh <- wantKM

	// Get key materials from the provider and compare it to the ones we pushed
	// above.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotKM, err := prov.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Close the provider and retry the KeyMaterial() call, and expect it to
	// fail with a known error.
	prov.Close()
	if _, err := prov.KeyMaterial(ctx, KeyMaterialOptions{}); !errors.Is(err, ErrProviderClosed) {
		t.Fatalf("provider.KeyMaterial() = %v, wantErr: %v", err, ErrProviderClosed)
	}
}

// TestStoreWithSingleProviderWithSharing creates multiple instances of the same
// type of provider through the store (and expects the store's sharing mechanism
// to kick in) and calls methods on it.
func (s) TestStoreWithSingleProviderWithSharing(t *testing.T) {
	prov1, fp1 := makeProvider(t, fakeProvider1Name, fakeConfig)
	prov2, _ := makeProvider(t, fakeProvider1Name, fakeConfig)

	// Push key materials into the fake provider1.
	wantKM, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	fp1.kmCh <- wantKM

	// Get key materials from the fake provider2 and compare it to the ones we
	// pushed above.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotKM, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Close the provider1 and retry the KeyMaterial() call on prov2, and expect
	// it to succeed.
	prov1.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}

	prov2.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); !errors.Is(err, ErrProviderClosed) {
		t.Fatalf("provider.KeyMaterial() = %v, wantErr: %v", err, ErrProviderClosed)
	}
}

// TestStoreWithSingleProviderWithoutSharing creates multiple instances of the
// same type of provider through the store with different configs. The store
// would end up creating different provider instances for these and no sharing
// would take place.
func (s) TestStoreWithSingleProviderWithoutSharing(t *testing.T) {
	prov1, fp1 := makeProvider(t, fakeProvider1Name, fakeConfig+"1111")
	prov2, fp2 := makeProvider(t, fakeProvider1Name, fakeConfig+"2222")

	// Push the same key materials into the two providers.
	wantKM, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	fp1.kmCh <- wantKM
	fp2.kmCh <- wantKM

	// Get key materials from the fake provider1 and compare it to the ones we
	// pushed above.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotKM, err := prov1.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Get key materials from the fake provider2 and compare it to the ones we
	// pushed above.
	gotKM, err = prov2.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Update the key materials used by provider1, and make sure provider2 is
	// not affected.
	newKM, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	newKM.Roots = nil
	fp1.kmCh <- newKM

	gotKM, err = prov2.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Close the provider1 and retry the KeyMaterial() call on prov2, and expect
	// it to succeed.
	prov1.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}

	prov2.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); !errors.Is(err, ErrProviderClosed) {
		t.Fatalf("provider.KeyMaterial() = %v, wantErr: %v", err, ErrProviderClosed)
	}
}

// TestStoreWithMultipleProviders creates multiple providers of different types
// and make sure closing of one does not affect the other.
func (s) TestStoreWithMultipleProviders(t *testing.T) {
	prov1, fp1 := makeProvider(t, fakeProvider1Name, fakeConfig)
	prov2, fp2 := makeProvider(t, fakeProvider2Name, fakeConfig)

	// Push key materials into the fake providers.
	wantKM, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	fp1.kmCh <- wantKM
	fp2.kmCh <- wantKM

	// Get key materials from the fake provider1 and compare it to the ones we
	// pushed above.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	gotKM, err := prov1.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Get key materials from the fake provider2 and compare it to the ones we
	// pushed above.
	gotKM, err = prov2.KeyMaterial(ctx, KeyMaterialOptions{})
	if err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}
	if !cmp.Equal(gotKM, wantKM, cmpopts.IgnoreUnexported(big.Int{}, x509.CertPool{})) {
		t.Fatalf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM)
	}

	// Close the provider1 and retry the KeyMaterial() call on prov2, and expect
	// it to succeed.
	prov1.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); err != nil {
		t.Fatalf("provider.KeyMaterial() = %v", err)
	}

	prov2.Close()
	if _, err := prov2.KeyMaterial(ctx, KeyMaterialOptions{}); !errors.Is(err, ErrProviderClosed) {
		t.Fatalf("provider.KeyMaterial() = %v, wantErr: %v", err, ErrProviderClosed)
	}
}
