/*
 *
 * Copyright 2023 gRPC authors.
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

package testutils

import (
	"sync"
	"testing"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ResolverClientConn is a fake implemetation of the resolver.ClientConn
// interface to be used in tests.
type ResolverClientConn struct {
	// Embedding the interface to avoid implementing deprecated methods.
	resolver.ClientConn

	logger  testingLogger
	StateCh chan resolver.State
	ErrorCh chan error

	mu             sync.Mutex
	updateStateErr error
}

// NewResolverClientConn creates a ResolverClientConn.
func NewResolverClientConn(t *testing.T) *ResolverClientConn {
	return &ResolverClientConn{
		logger: t,

		StateCh: make(chan resolver.State, 10),
		ErrorCh: make(chan error, 10),
	}
}

// UpdateState pushes the update received from the resolver on to StateCh.
func (t *ResolverClientConn) UpdateState(s resolver.State) error {
	t.logger.Logf("testutils.ResolverClientConn: UpdateState(%s)", pretty.ToJSON(s))

	select {
	case t.StateCh <- s:
	default:
	}

	t.mu.Lock()
	err := t.updateStateErr
	t.mu.Unlock()
	return err
}

// ReportError pushes the error received from the resolver on to ErrorCh.
func (t *ResolverClientConn) ReportError(err error) {
	t.logger.Logf("testutils.ResolverClientConn: ReportError(%v)", err)

	select {
	case t.ErrorCh <- err:
	default:
	}
}

// ParseServiceConfig parses the provided service by delegating the work to the
// implementation in the grpc package.
func (t *ResolverClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	return internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
}

// SetUpdateStateError sets the error to be returned by the ResolverClientConn
// when UpdateState is called by the Resolver implementation.
func (t *ResolverClientConn) SetUpdateStateError(err error) {
	t.mu.Lock()
	t.updateStateErr = err
	t.mu.Unlock()
}
