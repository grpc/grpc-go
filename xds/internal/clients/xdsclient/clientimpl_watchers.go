/*
 *
 * Copyright 2025 gRPC authors.
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
	"fmt"

	"google.golang.org/grpc/xds/internal/clients/xdsclient/internal/xdsresource"
)

// wrappingWatcher is a wrapper around an xdsresource.ResourceWatcher that adds
// the node ID to the error messages reported to the watcher.
type wrappingWatcher struct {
	ResourceWatcher
	nodeID string
}

func (w *wrappingWatcher) AmbientError(err error, done func()) {
	w.ResourceWatcher.AmbientError(fmt.Errorf("[xDS node id: %v]: %w", w.nodeID, err), done)
}

// WatchResource uses xDS to discover the resource associated with the provided
// resource name. The resource type implementation determines how xDS responses
// are are deserialized and validated, as received from the xDS management
// server. Upon receipt of a response from the management server, an
// appropriate callback on the watcher is invoked.
func (c *clientImpl) watchResource(typeURL, resourceName string, watcher ResourceWatcher) (cancel func()) {
	// Return early if the client is already closed.
	//
	// The client returned from the top-level API is a ref-counted client which
	// contains a pointer to `clientImpl`. When all references are released, the
	// ref-counted client sets its pointer to `nil`. And if any watch APIs are
	// made on such a closed client, we will get here with a `nil` receiver.

	rType, ok := c.config.ResourceTypes[typeURL]
	if !ok {
		logger.Warningf("ResourceType implementation for resource type url %v is not found", rType.TypeURL)
		watcher.ResourceError(fmt.Errorf("ResourceType implementation for resource type url %v is not found", rType.TypeURL), func() {})
		return func() {}
	}

	if c == nil || c.done.HasFired() {
		logger.Warningf("Watch registered for name %q of type %q, but client is closed", rType.TypeName, resourceName)
		return func() {}
	}

	watcher = &wrappingWatcher{
		ResourceWatcher: watcher,
		nodeID:          c.config.Node.ID,
	}

	n := xdsresource.ParseName(resourceName)
	a := c.getAuthorityForResource(n)
	if a == nil {
		logger.Warningf("Watch registered for name %q of type %q, authority %q is not found", rType.TypeName, resourceName, n.Authority)
		watcher.ResourceError(fmt.Errorf("authority %q not found in bootstrap config for resource %q", n.Authority, resourceName), func() {})
		return func() {}
	}
	// The watchResource method on the authority is invoked with n.String()
	// instead of resourceName because n.String() canonicalizes the given name.
	// So, two resource names which don't differ in the query string, but only
	// differ in the order of context params will result in the same resource
	// being watched by the authority.
	return a.watchResource(rType, n.String(), watcher)
}

// Gets the authority for the given resource name.
//
// See examples in this section of the gRFC:
// https://github.com/grpc/proposal/blob/master/A47-xds-federation.md#bootstrap-config-changes
func (c *clientImpl) getAuthorityForResource(name *xdsresource.Name) *authority {
	// For new-style resource names, always lookup the authorities map. If the
	// name does not specify an authority, we will end up looking for an entry
	// in the map with the empty string as the key.
	if name.Scheme == xdsresource.FederationScheme {
		return c.authorities[name.Authority]
	}

	// For old-style resource names, we use the top-level authority if the name
	// does not specify an authority.
	if name.Authority == "" {
		return c.topLevelAuthority
	}
	return c.authorities[name.Authority]
}

// A registry of ResourceType implementations indexed by their corresponding
// type URLs. Registration of an ResourceType happens the first time a watch
// for a resource of that type is invoked.
type resourceTypeRegistry struct {
	types map[string]ResourceType
}

func newResourceTypeRegistry(resourceTypes map[string]ResourceType) *resourceTypeRegistry {
	return &resourceTypeRegistry{types: resourceTypes}
}
