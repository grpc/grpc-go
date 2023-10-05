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
	"testing"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/serviceconfig"
)

// ResolverClientConn is a fake implemetation of the resolver.ClientConn
// interface to be used in tests.
type ResolverClientConn struct {
	resolver.ClientConn // Embedding the interface to avoid implementing deprecated methods.
	logger              testingLogger

	// Callbacks specified by the test.
	updateStateF func(resolver.State) error
	reportErrorF func(err error)
}

// NewResolverClientConn creates a ResolverClientConn.
//
// updateState and reportError, when non-nil, are callbacks that will be invoked
// by the ResolverClientConn when the resolver pushes an update or reports an
// error respectively.
func NewResolverClientConn(t *testing.T, updateState func(resolver.State) error, reportError func(error)) *ResolverClientConn {
	return &ResolverClientConn{
		logger:       t,
		updateStateF: updateState,
		reportErrorF: reportError,
	}
}

// UpdateState invokes the test specified callback with the update received from
// the resolver. If the callback returns a non-nil error, the same will be
// propagated to the resolver.
func (t *ResolverClientConn) UpdateState(s resolver.State) error {
	t.logger.Logf("testutils.ResolverClientConn: UpdateState(%s)", pretty.ToJSON(s))

	if t.updateStateF != nil {
		return t.updateStateF(s)
	}
	return nil
}

// ReportError pushes the error received from the resolver on to ErrorCh.
func (t *ResolverClientConn) ReportError(err error) {
	t.logger.Logf("testutils.ResolverClientConn: ReportError(%v)", err)

	if t.reportErrorF != nil {
		t.reportErrorF(err)
	}
}

// ParseServiceConfig parses the provided service by delegating the work to the
// implementation in the grpc package.
func (t *ResolverClientConn) ParseServiceConfig(jsonSC string) *serviceconfig.ParseResult {
	return internal.ParseServiceConfig.(func(string) *serviceconfig.ParseResult)(jsonSC)
}
