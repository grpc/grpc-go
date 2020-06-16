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
	"reflect"
	"sync"
	"testing"
	"time"
)

var errProviderTestInternal = errors.New("provider internal error")

// TestDistributor invokes the different methods on the Distributor type and
// verifies the results.
func (s) TestDistributor(t *testing.T) {
	dist := NewDistributor()

	// Read cert/key files from testdata.
	km, err := loadKeyMaterials()
	if err != nil {
		t.Fatal(err)
	}
	// wantKM1 has both local and root certs.
	wantKM1 := *km
	// wantKM2 has only local certs. Roots are nil-ed out.
	wantKM2 := *km
	wantKM2.Roots = nil

	// Create a goroutines which work in lockstep with the rest of the test.
	// This goroutine reads the key material from the distributor while the rest
	// of the test sets it.
	var wg sync.WaitGroup
	wg.Add(1)
	errCh := make(chan error)
	proceedCh := make(chan struct{})
	go func() {
		defer wg.Done()

		// The first call to KeyMaterial() should timeout because no key
		// material has been set on the distributor as yet.
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout/2)
		defer cancel()
		if _, err := dist.KeyMaterial(ctx); err != context.DeadlineExceeded {
			errCh <- err
			return
		}
		proceedCh <- struct{}{}

		// This call to KeyMaterial() should return the key material with both
		// the local certs and the root certs.
		ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		gotKM, err := dist.KeyMaterial(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if !reflect.DeepEqual(gotKM, &wantKM1) {
			errCh <- fmt.Errorf("provider.KeyMaterial() = %+v, want %+v", gotKM, wantKM1)
		}
		proceedCh <- struct{}{}

		// This call to KeyMaterial() should eventually return key material with
		// only the local certs.
		ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		for {
			gotKM, err := dist.KeyMaterial(ctx)
			if err != nil {
				errCh <- err
				return
			}
			if reflect.DeepEqual(gotKM, &wantKM2) {
				break
			}
		}
		proceedCh <- struct{}{}

		// This call to KeyMaterial() should return nil key material and a
		// non-nil error.
		ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		for {
			gotKM, err := dist.KeyMaterial(ctx)
			if gotKM == nil && err == errProviderTestInternal {
				break
			}
			if err != nil {
				// If we have gotten any error other than
				// errProviderTestInternal, we should bail out.
				errCh <- err
				return
			}
		}
		proceedCh <- struct{}{}

		// This call to KeyMaterial() should eventually return errProviderClosed
		// error.
		ctx, cancel = context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		for {
			if _, err := dist.KeyMaterial(ctx); err == errProviderClosed {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	waitAndDo(t, proceedCh, errCh, func() {
		dist.Set(&wantKM1, nil)
	})

	waitAndDo(t, proceedCh, errCh, func() {
		dist.Set(&wantKM2, nil)
	})

	waitAndDo(t, proceedCh, errCh, func() {
		dist.Set(&wantKM2, errProviderTestInternal)
	})

	waitAndDo(t, proceedCh, errCh, func() {
		dist.Stop()
	})

}

func waitAndDo(t *testing.T, proceedCh chan struct{}, errCh chan error, do func()) {
	t.Helper()

	timer := time.NewTimer(defaultTestTimeout)
	select {
	case <-timer.C:
		t.Fatalf("test timed out when waiting for event from distributor")
	case <-proceedCh:
		do()
	case err := <-errCh:
		t.Fatal(err)
	}
}
