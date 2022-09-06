/*
 *
 * Copyright 2021 gRPC authors.
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
 */

package xdsclient

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/load"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
	"google.golang.org/protobuf/types/known/anypb"
)

type watchState int

const (
	watchStateStarted watchState = iota
	watchStateRespReceived
	watchStateTimeout
	watchStateCanceled
)

type resourceState struct {
	watchers map[ResourceWatcher]bool   // Set of watchers for this resource
	cache    xdsresource.ResourceData   // Most recent ACKed update for this resource
	md       xdsresource.UpdateMetadata // Metadata for the most recent update

	// Common watch state for all watchers of this resource.
	wTimer *time.Timer // Expiry timer
	wState watchState  // State of the watch
}

// authority is a combination of pubsub and the controller for this authority.
//
// Note that it might make sense to use one pubsub for all the resources (for
// all the controllers). One downside is the handling of StoW APIs (LDS/CDS).
// These responses contain all the resources from that control plane, so pubsub
// will need to keep lists of resources from each control plane, to know what
// are removed.
type authority struct {
	config             *bootstrap.ServerConfig       // Server config for this authority
	bootstrapCfg       *bootstrap.Config             // Full bootstrap configuration
	refCount           int                           // Reference count of watches referring to this authority
	serializer         *CallbackSerializer           // Callback serializer for invoking watch callbacks
	resourceTypeGetter func(string) xdsresource.Type // ResourceType registry lookup
	transport          *transport.Transport          // Underlying xDS transport to the management server
	watchExpiryTimeout time.Duration                 // Resource watch expiry timeout
	logger             *grpclog.PrefixLogger

	// A two level map containing the state of all the resources being watched.
	//
	// The first level map key is the ResourceType (Listener, Route etc). This
	// allows us to have a single map for all resources instead of having per
	// resource-type maps.
	//
	// The second level map key is the resource name, with the value being the
	// actual state of the resource.
	resourcesMu sync.Mutex
	resources   map[xdsresource.Type]map[string]*resourceState
}

func (a *authority) handleResourceUpdate(resourceUpdate transport.ResourceUpdate) error {
	rType := a.resourceTypeGetter(resourceUpdate.URL)
	if rType == nil {
		return xdsresource.ErrResourceTypeUnsupported{ErrStr: fmt.Sprintf("Resource URL %v unknown in response from server", resourceUpdate.URL)}
	}

	opts := &xdsresource.DecodeOptions{
		BootstrapConfig: a.bootstrapCfg,
		Logger:          a.logger,
	}
	updates, md, err := decodeAllResources(opts, rType, resourceUpdate)
	a.updateResourceStateAndScheduleCallbacks(rType, updates, md)
	return err
}

func (a *authority) updateResourceStateAndScheduleCallbacks(rType xdsresource.Type, updates map[string]resourceDataErrTuple, md xdsresource.UpdateMetadata) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()

	resourceStates := a.resources[rType]
	for name, uErr := range updates {
		if state, ok := resourceStates[name]; ok {
			// Cancel the expiry timer associated with the resource once a
			// response is received, irrespective of whether the update is a
			// good one or not.
			state.wTimer.Stop()
			state.wState = watchStateRespReceived

			if uErr.err != nil {
				// On error, keep previous version of the resource. But update
				// status and error.
				state.md.ErrState = md.ErrState
				state.md.Status = md.Status
				for watcher := range state.watchers {
					watcher := watcher
					err := uErr.err
					a.serializer.Schedule(func() { watcher.OnError(err) })
				}
				continue
			}
			// If we get here, it means that the update is a valid one. Notify
			// watchers only if this is a first time update or it is different
			// from the one currently cached.
			if state.cache == nil || !state.cache.Equal(uErr.resource) {
				for watcher := range state.watchers {
					watcher := watcher
					resource := uErr.resource
					a.serializer.Schedule(func() { watcher.OnResourceChanged(resource) })
				}
			}
			// Sync cache.
			a.logger.Debugf("Resource type %q with name %q, value %s added to cache", rType.TypeEnum().String(), name, uErr.resource.ToJSON())
			state.cache = uErr.resource
			// Set status to ACK, and clear error state. The metadata might be a
			// NACK metadata because some other resources in the same response
			// are invalid.
			state.md = md
			state.md.ErrState = nil
			state.md.Status = xdsresource.ServiceStatusACKed
			if md.ErrState != nil {
				state.md.Version = md.ErrState.Version
			}
		}
	}

	// If this resource type requires that all resources be present in every
	// SotW response from the server, a response that does not include a
	// previously seen resource will be interpreted as a deletion of that
	// resource.
	if !rType.AllResourcesRequiredInSotW() {
		return
	}
	for name, state := range resourceStates {
		if _, ok := updates[name]; !ok {
			// The metadata status is set to "ServiceStatusNotExist" if a
			// previous update deleted this resource, in which case we do not
			// want to repeatedly call the watch callbacks with a
			// "resource-not-found" error.
			if state.md.Status == xdsresource.ServiceStatusNotExist {
				continue
			}

			// If resource exists in cache, but not in the new update, delete
			// the resource from cache, and also send a resource not found error
			// to indicate resource removed. Metadata for the resource is still
			// maintained, as this is required by CSDS.
			state.cache = nil
			state.md = xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusNotExist}
			for watcher := range state.watchers {
				watcher := watcher
				a.serializer.Schedule(func() { watcher.OnResourceDoesNotExist() })
			}
		}
	}
}

type resourceDataErrTuple struct {
	resource xdsresource.ResourceData
	err      error
}

func decodeAllResources(opts *xdsresource.DecodeOptions, rType xdsresource.Type, update transport.ResourceUpdate) (map[string]resourceDataErrTuple, xdsresource.UpdateMetadata, error) {
	timestamp := time.Now()
	md := xdsresource.UpdateMetadata{
		Version:   update.Version,
		Timestamp: timestamp,
	}

	topLevelErrors := make([]error, 0)           // Tracks deserialization errors, where we don't have a resource name.
	perResourceErrors := make(map[string]error)  // Tracks resource validation errors, where we have a resource name.
	ret := make(map[string]resourceDataErrTuple) // Return result, a map from resource name to either resource data or error.
	for _, r := range update.Resources {
		result, err := rType.Decode(opts, r)

		// Name field of the result is left unpopulated only when resource
		// deserialization fails.
		name := ""
		if result != nil {
			name = xdsresource.ParseName(result.Name).String()
		}
		if err == nil {
			ret[name] = resourceDataErrTuple{resource: result.Resource}
			continue
		}
		if name == "" {
			topLevelErrors = append(topLevelErrors, err)
			continue
		}
		perResourceErrors[name] = err
		// Add place holder in the map so we know this resource name was in
		// the response.
		ret[name] = resourceDataErrTuple{err: err}
	}

	if len(topLevelErrors) == 0 && len(perResourceErrors) == 0 {
		md.Status = xdsresource.ServiceStatusACKed
		return ret, md, nil
	}

	typeStr := rType.TypeEnum().String()
	md.Status = xdsresource.ServiceStatusNACKed
	errRet := xdsresource.CombineErrors(typeStr, topLevelErrors, perResourceErrors)
	md.ErrState = &xdsresource.UpdateErrorMetadata{
		Version:   update.Version,
		Err:       errRet,
		Timestamp: timestamp,
	}
	return ret, md, errRet
}

// newConnectionError is called by the underlying transport when it receives a
// connection error. The error will be forwarded to all the resource watchers.
func (a *authority) newConnectionError(err error) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()

	// For all resource types, for all resources within each resource type, and
	// for all the watchers for every resource, propagate the connection error
	// from the transport layer.
	for _, rType := range a.resources {
		for _, state := range rType {
			for watcher := range state.watchers {
				watcher := watcher
				a.serializer.Schedule(func() {
					watcher.OnError(xdsresource.NewErrorf(xdsresource.ErrorTypeConnection, "xds: error received from xDS stream: %v", err))
				})
			}
		}
	}
}

// caller must hold parent's authorityMu.
func (a *authority) ref() {
	a.refCount++
}

// caller must hold parent's authorityMu.
func (a *authority) unref() int {
	a.refCount--
	return a.refCount
}

func (a *authority) close() {
	if a.transport != nil {
		a.transport.Close()
	}
}

func (a *authority) watchResource(rType xdsresource.Type, resourceName string, watcher ResourceWatcher) func() {
	a.logger.Debugf("New watch for type %q, resource name %q", rType.TypeEnum(), resourceName)
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()

	// Lookup the ResourceType specific resources from the top-level map. If
	// there is no entry for this ResourceType, create one.
	resources := a.resources[rType]
	if resources == nil {
		resources = make(map[string]*resourceState)
		a.resources[rType] = resources
	}

	// Lookup the resourceState for the particular resource that the watch is
	// being registered for. If this is the first watch for this resource,
	// instruct the transport layer to send a DiscoveryRequest for the same.
	state := resources[resourceName]
	if state == nil {
		a.logger.Debugf("First watch for type %q, resource name %q", rType.TypeEnum(), resourceName)
		state = &resourceState{
			watchers: make(map[ResourceWatcher]bool),
			md:       xdsresource.UpdateMetadata{Status: xdsresource.ServiceStatusRequested},
			wState:   watchStateStarted,
		}
		state.wTimer = time.AfterFunc(a.watchExpiryTimeout, func() {
			a.handleWatchTimerExpiry(state, fmt.Errorf("watch for resource %q of type %s timed out", resourceName, rType.TypeEnum().String()))
		})
		resources[resourceName] = state
		a.sendDiscoveryRequest(rType, resources)
	}
	// Always add the new watcher to the set of watchers.
	state.watchers[watcher] = true

	// If we have a cached copy of the resource, notify the new watcher.
	if state.cache != nil {
		a.logger.Debugf("Resource type %q with resource name %q found in cache: %s", rType.TypeEnum(), resourceName, state.cache.ToJSON())
		a.serializer.Schedule(func() { watcher.OnResourceChanged(state.cache) })
	}

	return func() {
		a.resourcesMu.Lock()
		defer a.resourcesMu.Unlock()

		// We already have a reference to the resourceState for this particular
		// resource. Avoid indexing into the two-level map to figure this out.

		// Delete this particular watcher from the list of watchers, so that its
		// callback will not be invoked in the future.
		state.wState = watchStateCanceled
		delete(state.watchers, watcher)
		if len(state.watchers) > 0 {
			return
		}

		// There are no more watchers for this resource, delete the state
		// associated with it, and instruct the transport to send a request
		// which does not include this resource name.
		delete(resources, resourceName)
		a.sendDiscoveryRequest(rType, resources)
	}
}

func (a *authority) handleWatchTimerExpiry(state *resourceState, err error) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()

	if state.wState == watchStateCanceled {
		return
	}

	state.wState = watchStateTimeout
	for watcher := range state.watchers {
		watcher := watcher
		a.serializer.Schedule(func() { watcher.OnError(err) })
	}
}

func (a *authority) sendDiscoveryRequest(rType xdsresource.Type, resources map[string]*resourceState) {
	resourcesToRequest := make([]string, len(resources))
	i := 0
	for name := range resources {
		resourcesToRequest[i] = name
		i++
	}
	a.transport.SendRequest(rType.TypeEnum(), resourcesToRequest)
}

func (a *authority) reportLoad() (*load.Store, func()) {
	return a.transport.ReportLoad()
}

func (a *authority) dumpResources() map[xdsresource.Type]map[string]xdsresource.UpdateWithMD {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()

	dump := make(map[xdsresource.Type]map[string]xdsresource.UpdateWithMD)
	for rType, resourceStates := range a.resources {
		states := make(map[string]xdsresource.UpdateWithMD)
		for name, state := range resourceStates {
			var raw *anypb.Any
			if state.cache != nil {
				raw = state.cache.Raw()
			}
			states[name] = xdsresource.UpdateWithMD{
				MD:  state.md,
				Raw: raw,
			}
		}
		dump[rType] = states
	}
	return dump
}
