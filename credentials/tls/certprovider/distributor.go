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
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
)

// Distributor makes it easy for provider implementations to furnish new key
// materials by handling synchronization between the producer and consumers of
// the key material. Distributor implements the Provider interface.
//
// Provider implementations may choose to embed the Distributor and:
// - Whenever they have new key material, they invoke the Set() method.
// - When users of the provider call KeyMaterial(), it will be handled by the
//   distributor which will return the most up-to-date key material furnished
//   by the provider.
// - When users of the provider call Close(), it will be handled by the
//   distributor and the exposed Ctx will be canceled. Provider implementations
//   can select on the channel returned by Ctx.Done() to perform cleanup work.
type Distributor struct {
	// Ctx is a context used to signal cancellation of the distributor. Users of
	// this type can select on the channel returned by Ctx.Done to perform any
	// cleanup work.
	Ctx    context.Context
	cancel context.CancelFunc

	// mu protects the underlying key material.
	mu sync.Mutex
	km *KeyMaterial

	ready  *grpcsync.Event
	closed *grpcsync.Event
}

// NewDistributor returns a new Distributor.
func NewDistributor() *Distributor {
	ctx, cancel := context.WithCancel(context.Background())
	return &Distributor{
		Ctx:    ctx,
		cancel: cancel,
		ready:  grpcsync.NewEvent(),
		closed: grpcsync.NewEvent(),
	}
}

// Set updates the key material in the distributor with km.
//
// Provider implementations which use the distributor must not modify the
// contents of the KeyMaterial struct pointed to by km.
func (d *Distributor) Set(km *KeyMaterial) {
	d.mu.Lock()
	d.km = km
	d.ready.Fire()
	d.mu.Unlock()
}

// KeyMaterial returns the most recent key material provided to the distributor.
// If no key material was provided to the distributor at the time of this call,
// it will block until the deadline on the context expires or fresh key material
// arrives.
func (d *Distributor) KeyMaterial(ctx context.Context, opts KeyMaterialOptions) (*KeyMaterial, error) {
	if d.closed.HasFired() {
		return nil, errProviderClosed
	}

	if d.ready.HasFired() {
		return d.keyMaterial(), nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.closed.Done():
		return nil, errProviderClosed
	case <-d.ready.Done():
		return d.keyMaterial(), nil
	}
}

func (d *Distributor) keyMaterial() *KeyMaterial {
	d.mu.Lock()
	km := d.km
	d.mu.Unlock()
	return km
}

// Close closes the distributor and fails any active KeyMaterial() call waiting
// for new key material. It also cancels the exposed Ctx field.
func (d *Distributor) Close() {
	d.closed.Fire()
	d.cancel()
}
