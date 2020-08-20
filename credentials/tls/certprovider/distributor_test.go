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
	"errors"
	"fmt"
	"testing"
	"time"
)

var errProviderTestInternal = errors.New("provider internal error")

// TestDistributorEmpty tries to read key material from an empty distributor and
// expects the call to timeout.
func (s) TestDistributorEmpty(t *testing.T) {
	dist := NewDistributor()

	// This call to KeyMaterial() should timeout because no key material has
	// been set on the distributor as yet.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := readAndVerifyKeyMaterial(ctx, dist, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// TestDistributor invokes the different methods on the Distributor type and
// verifies the results.
func (s) TestDistributor(t *testing.T) {
	dist := NewDistributor()

	// Read cert/key files from testdata.
	km1 := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	km2 := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")

	// Push key material into the distributor and make sure that a call to
	// KeyMaterial() returns the expected key material, with both the local
	// certs and root certs.
	dist.Set(km1, nil)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if err := readAndVerifyKeyMaterial(ctx, dist, km1); err != nil {
		t.Fatal(err)
	}

	// Push new key material into the distributor and make sure that a call to
	// KeyMaterial() returns the expected key material, with only root certs.
	dist.Set(km2, nil)
	if err := readAndVerifyKeyMaterial(ctx, dist, km2); err != nil {
		t.Fatal(err)
	}

	// Push an error into the distributor and make sure that a call to
	// KeyMaterial() returns that error and nil keyMaterial.
	dist.Set(km2, errProviderTestInternal)
	if gotKM, err := dist.KeyMaterial(ctx); gotKM != nil || !errors.Is(err, errProviderTestInternal) {
		t.Fatalf("KeyMaterial() = {%v, %v}, want {nil, %v}", gotKM, err, errProviderTestInternal)
	}

	// Stop the distributor and KeyMaterial() should return errProviderClosed.
	dist.Stop()
	if km, err := dist.KeyMaterial(ctx); !errors.Is(err, errProviderClosed) {
		t.Fatalf("KeyMaterial() = {%v, %v}, want {nil, %v}", km, err, errProviderClosed)
	}
}

// TestDistributorConcurrency invokes methods on the distributor in parallel.
func (s) TestDistributorConcurrency(t *testing.T) {
	dist := NewDistributor()

	// Read cert/key files from testdata.
	km1 := loadKeyMaterials(t, "x509/server1_cert.pem", "x509/server1_key.pem", "x509/client_ca_cert.pem")
	km2 := loadKeyMaterials(t, "x509/server2_cert.pem", "x509/server2_key.pem", "x509/client_ca_cert.pem")

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	tests := []struct {
		km  *KeyMaterial
		err error
	}{
		{km: km1, err: nil},
		{km: km2, err: nil},
		{km: nil, err: errProviderTestInternal},
	}
	for _, tc := range tests {
		// Push key material into the distributor from a goroutine and read from
		// here to verify that the distributor returns the expected keyMaterial.
		go func() {
			// Add a small sleep here to make sure that the call to KeyMaterial()
			// happens before the call to Set(), thereby the former is blocked till
			// the latter happens.
			time.Sleep(50 * time.Microsecond)
			dist.Set(tc.km, tc.err)
		}()
		if err := waitForKeyMaterial(ctx, dist, tc.km, tc.err); err != nil {
			t.Fatal(err)
		}
	}
}

// waitForKeyMaterial reads key material from the given distributor until one of
// the following conditions are met:
// 1. Returned keyMaterial and error matches wantKM and wantErr.
// 2. Provider ctx deadline expires.
func waitForKeyMaterial(ctx context.Context, dist *Distributor, wantKM *KeyMaterial, wantErr error) error {
	for {
		err := readAndVerifyKeyMaterial(ctx, dist, wantKM)
		if errors.Is(err, wantErr) {
			return nil

		}
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("KeyMaterial() failed with error: %v, wantErr: %v", err, wantErr)
		}
		// Don't busy loop.
		time.Sleep(50 * time.Microsecond)
	}
}
