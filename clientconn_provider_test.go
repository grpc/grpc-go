/*
 *
 * Copyright 2026 gRPC authors.
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

package grpc

import (
	"errors"
	"sync"
	"testing"

	"google.golang.org/grpc/credentials/insecure"
)

// fakeClientConn is a ClientConnInterface used to identify the ClientConn
// returned by a provider.
type fakeClientConn struct {
	ClientConnInterface
	key string
	id  int
}

// fakeProvider is a ClientConnProvider that creates a new fakeClientConn on
// every call (or returns the configured error), and records the number of calls
// and cancellations per key.
type fakeProvider struct {
	mu      sync.Mutex
	calls   map[string]int
	cancels map[string]int
	err     error // If non-nil, returned by every call.
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{calls: make(map[string]int), cancels: make(map[string]int)}
}

func (p *fakeProvider) provide(key string) (ClientConnInterface, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls[key]++
	if p.err != nil {
		return nil, nil, p.err
	}
	cc := &fakeClientConn{key: key, id: p.calls[key]}
	return cc, func() {
		p.mu.Lock()
		p.cancels[key]++
		p.mu.Unlock()
	}, nil
}

func (p *fakeProvider) counts(key string) (calls, cancels int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[key], p.cancels[key]
}

func verifyCounts(t *testing.T, p *fakeProvider, key string, wantCalls, wantCancels int) {
	t.Helper()
	if calls, cancels := p.counts(key); calls != wantCalls || cancels != wantCancels {
		t.Fatalf("For key %q, provider called %d times and cancelled %d times, want %d and %d", key, calls, cancels, wantCalls, wantCancels)
	}
}

// Tests that the shared provider returns the same ClientConn to callers
// requesting the same key, invokes the wrapped provider only once while there
// are outstanding references, and invokes the cancel function returned by the
// wrapped provider only when the last reference is released. Also verifies
// that the cancel function returned to callers is idempotent, and that a new
// ClientConn is created once all references to the previous one are released.
func (s) TestSharedClientConnProvider_SharesByKey(t *testing.T) {
	fp := newFakeProvider()
	ccp := newSharedClientConnProvider(fp.provide)

	cc1, cancel1, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	cc2, cancel2, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	if cc1 != cc2 {
		t.Fatalf("ccp(%q) returned different ClientConns %v and %v, want the same", "a", cc1, cc2)
	}
	verifyCounts(t, fp, "a", 1, 0)

	// Releasing one reference, even multiple times, must not close the
	// shared ClientConn.
	cancel1()
	cancel1()
	verifyCounts(t, fp, "a", 1, 0)

	// Releasing the last reference must close it.
	cancel2()
	verifyCounts(t, fp, "a", 1, 1)

	// A subsequent request must create a new ClientConn.
	cc3, cancel3, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	if cc3 == cc1 {
		t.Fatalf("ccp(%q) returned the previously closed ClientConn %v", "a", cc3)
	}
	verifyCounts(t, fp, "a", 2, 1)
	cancel3()
	verifyCounts(t, fp, "a", 2, 2)
}

// Tests that the shared provider does not share ClientConns across keys.
func (s) TestSharedClientConnProvider_DifferentKeys(t *testing.T) {
	fp := newFakeProvider()
	ccp := newSharedClientConnProvider(fp.provide)

	ccA, cancelA, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	ccB, cancelB, err := ccp("b")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "b", err)
	}
	if ccA == ccB {
		t.Fatalf("ccp() returned the same ClientConn %v for different keys", ccA)
	}
	verifyCounts(t, fp, "a", 1, 0)
	verifyCounts(t, fp, "b", 1, 0)

	cancelA()
	verifyCounts(t, fp, "a", 1, 1)
	verifyCounts(t, fp, "b", 1, 0)
	cancelB()
	verifyCounts(t, fp, "b", 1, 1)
}

// Tests that errors returned by the delegate provider are not cached, i.e., the
// delegate provider is invoked again on a subsequent request for the same key.
func (s) TestSharedClientConnProvider_ErrorNotCached(t *testing.T) {
	fp := newFakeProvider()
	wantErr := errors.New("provider error")
	fp.err = wantErr
	ccp := newSharedClientConnProvider(fp.provide)

	for i := 1; i <= 2; i++ {
		if _, _, err := ccp("a"); !errors.Is(err, wantErr) {
			t.Fatalf("ccp(%q) returned error %v, want %v", "a", err, wantErr)
		}
		verifyCounts(t, fp, "a", i, 0)
	}
}

// Tests that concurrent requests and releases for the same key do not race, and
// that every ClientConn created by the wrapped provider is cancelled exactly
// once after all references are released. This exercises the case where a
// request arrives after the reference count of the existing ClientConn has
// dropped to zero, but before it is removed from the map.
func (s) TestSharedClientConnProvider_Concurrent(t *testing.T) {
	fp := newFakeProvider()
	ccp := newSharedClientConnProvider(fp.provide)

	const numGoroutines = 10
	const numIterations = 1000
	var wg sync.WaitGroup
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range numIterations {
				cc, cancel, err := ccp("a")
				if err != nil {
					t.Errorf("ccp(%q) failed: %v", "a", err)
					return
				}
				if cc == nil {
					t.Errorf("ccp(%q) returned a nil ClientConn", "a")
				}
				cancel()
			}
		}()
	}
	wg.Wait()

	calls, cancels := fp.counts("a")
	if calls == 0 || calls != cancels {
		t.Fatalf("Provider called %d times and cancelled %d times, want equal and non-zero", calls, cancels)
	}
}

// Tests that the ClientConnProvider passed to WithClientConnProvider is wrapped
// once per channel, such that ClientConns are shared by callers requesting the
// same key.
func (s) TestWithClientConnProvider_SharesClientConns(t *testing.T) {
	fp := newFakeProvider()
	cc, err := NewClient("passthrough:///test.server", WithTransportCredentials(insecure.NewCredentials()), WithClientConnProvider(fp.provide))
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer cc.Close()

	ccp := cc.dopts.clientConnProvider
	cc1, cancel1, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	cc2, cancel2, err := ccp("a")
	if err != nil {
		t.Fatalf("ccp(%q) failed: %v", "a", err)
	}
	if cc1 != cc2 {
		t.Fatalf("ccp(%q) returned different ClientConns %v and %v, want the same", "a", cc1, cc2)
	}
	verifyCounts(t, fp, "a", 1, 0)
	cancel1()
	cancel2()
	verifyCounts(t, fp, "a", 1, 1)
}
