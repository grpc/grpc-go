/*
 *
 * Copyright 2022 gRPC authors.
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

package xdsclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/cache"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/xds/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// NameForServer represents the value to be passed as name when creating an xDS
// client from xDS-enabled gRPC servers. This is a well-known dedicated key
// value, and is defined in gRFC A71.
const NameForServer = "#server"

// New returns a new xDS client configured by the bootstrap file specified in env
// variable GRPC_XDS_BOOTSTRAP or GRPC_XDS_BOOTSTRAP_CONFIG.
//
// gRPC client implementations are expected to pass the channel's target URI for
// the name field, while server implementations are expected to pass a dedicated
// well-known value "#server", as specified in gRFC A71. The returned client is
// a reference counted implementation shared among callers using the same name.
//
// The second return value represents a close function which releases the
// caller's reference on the returned client.  The caller is expected to invoke
// it once they are done using the client. The underlying client will be closed
// only when all references are released, and it is safe for the caller to
// invoke this close function multiple times.
func New(name string) (XDSClient, func(), error) {
	return NewWithConfig(name, nil)
}

// NewWithConfig is similar to New, except that it uses the provided bootstrap
// configuration to create the xDS client if and only if the bootstrap
// environment variables are not defined.
//
// # Internal Only
//
// This function should ONLY be used by the internal google-c2p resolver.
// DO NOT use this elsewhere. Use New() instead.
func NewWithConfig(name string, config *bootstrap.Config) (XDSClient, func(), error) {
	return newRefCountedWithConfig(name, config, defaultWatchExpiryTimeout, defaultIdleAuthorityDeleteTimeout)
}

// newClientImpl returns a new xdsClient with the given config.
func newClientImpl(config *bootstrap.Config, watchExpiryTimeout time.Duration, idleAuthorityDeleteTimeout time.Duration) (*clientImpl, error) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &clientImpl{
		done:               grpcsync.NewEvent(),
		config:             config,
		watchExpiryTimeout: watchExpiryTimeout,
		serializer:         grpcsync.NewCallbackSerializer(ctx),
		serializerClose:    cancel,
		resourceTypes:      newResourceTypeRegistry(),
		authorities:        make(map[string]*authority),
		idleAuthorities:    cache.NewTimeoutCache(idleAuthorityDeleteTimeout),
	}

	c.logger = prefixLogger(c)
	return c, nil
}

// OptionsForTesting contains options to configure xDS client creation for
// testing purposes only.
type OptionsForTesting struct {
	// Name is a unique name for this xDS client.
	Name string
	// Contents contain a JSON representation of the bootstrap configuration to
	// be used when creating the xDS client.
	Contents []byte

	// WatchExpiryTimeout is the timeout for xDS resource watch expiry. If
	// unspecified, uses the default value used in non-test code.
	WatchExpiryTimeout time.Duration

	// AuthorityIdleTimeout is the timeout before idle authorities are deleted.
	// If unspecified, uses the default value used in non-test code.
	AuthorityIdleTimeout time.Duration
}

// NewForTesting returns an xDS client configured with the provided options.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func NewForTesting(opts OptionsForTesting) (XDSClient, func(), error) {
	if opts.Name == "" {
		return nil, nil, fmt.Errorf("opts.Name field must be non-empty")
	}
	if opts.WatchExpiryTimeout == 0 {
		opts.WatchExpiryTimeout = defaultWatchExpiryTimeout
	}
	if opts.AuthorityIdleTimeout == 0 {
		opts.AuthorityIdleTimeout = defaultIdleAuthorityDeleteTimeout
	}

	cfg, err := bootstrap.NewConfigFromContents(opts.Contents)
	if err != nil {
		return nil, nil, err
	}
	return newRefCountedWithConfig(opts.Name, cfg, opts.WatchExpiryTimeout, opts.AuthorityIdleTimeout)
}

// GetForTesting returns an xDS client created earlier using the given name.
//
// The second return value represents a close function which the caller is
// expected to invoke once they are done using the client.  It is safe for the
// caller to invoke this close function multiple times.
//
// # Testing Only
//
// This function should ONLY be used for testing purposes.
func GetForTesting(name string) (XDSClient, func(), error) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	c, ok := clients[name]
	if !ok {
		return nil, nil, fmt.Errorf("xDS client with name %q not found", name)
	}
	c.incrRef()
	return c, grpcsync.OnceFunc(func() { clientRefCountedClose(name) }), nil
}

func init() {
	internal.TriggerXDSResourceNotFoundForTesting = triggerXDSResourceNotFoundForTesting
}

func triggerXDSResourceNotFoundForTesting(client XDSClient, typ xdsresource.Type, name string) error {
	crc, ok := client.(*clientRefCounted)
	if !ok {
		return fmt.Errorf("xDS client is of type %T, want %T", client, &clientRefCounted{})
	}
	return crc.clientImpl.triggerResourceNotFoundForTesting(typ, name)
}

var (
	clients   = map[string]*clientRefCounted{}
	clientsMu sync.Mutex
)
