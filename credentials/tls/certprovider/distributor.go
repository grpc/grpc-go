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
	"sync"

	"google.golang.org/grpc/internal/grpcsync"
)

// Distributor makes it easy for provider implementations to furnish new key
// materials by handling synchronization between the producer and consumers of
// the key material.
//
// Provider implementations must embed this type into themselves and:
// - Whenever they have new key material, they should invoke the Set() method.
// - When users of the provider call KeyMaterial(), it will be handled by the
//   distributor which will return the most up-to-date key material furnished
//   by the provider.
// - When users of the provider call Close(), the channel returned by the
//   Done() method will be closed. So, provider implementations can select on
//   the channel returned by Done() to perform cleanup work.
type Distributor struct {
	mu     sync.Mutex
	certs  []tls.Certificate
	roots  *x509.CertPool
	ready  *grpcsync.Event
	closed *grpcsync.Event
}

// NewDistributor returns a new Distributor.
func NewDistributor() *Distributor {
	return &Distributor{
		ready:  grpcsync.NewEvent(),
		closed: grpcsync.NewEvent(),
	}
}

// Set updates the key material in the distributor with km.
func (d *Distributor) Set(km *KeyMaterial) {
	d.mu.Lock()
	d.certs = km.Certs
	d.roots = km.Roots
	d.ready.Fire()
	d.mu.Unlock()
}

// KeyMaterial returns the most recent key material provided to the distributor.
// If no key material was provided to the distributor at the time of this call,
// it will block until the deadline on the context expires or fresh key material
// arrives.
func (d *Distributor) KeyMaterial(ctx context.Context, opts KeyMaterialOptions) (*KeyMaterial, error) {
	if d.closed.HasFired() {
		return nil, ErrProviderClosed
	}

	if d.ready.HasFired() {
		return d.keyMaterial(), nil
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.closed.Done():
		return nil, ErrProviderClosed
	case <-d.ready.Done():
		return d.keyMaterial(), nil
	}
}

func (d *Distributor) keyMaterial() *KeyMaterial {
	d.mu.Lock()
	km := &KeyMaterial{Certs: d.certs, Roots: d.roots}
	d.mu.Unlock()
	return km
}

// Close closes the distributor and fails any active KeyMaterial() call waiting
// for new key material. It also closes the channel returned by Done().
func (d *Distributor) Close() {
	d.closed.Fire()
}

// Done returns a channel which is closed when the distributor is closed.
//
// Provider implementations which embed the distributor should select on this
// channel to perform any cleanup work.
func (d *Distributor) Done() <-chan struct{} {
	return d.closed.Done()
}
