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

// ResourceWatcher is notified of the resource updates and errors that are
// received by the xDS client from the management server.
//
// All methods on this interface are guaranteed to be called serially by the xDS
// client.
//
// All methods contain a done parameter which should be called when processing
// of the update has completed.  For example, if processing a resource requires
// watching new resources, those watches should be completed before done is
// called, which can happen after the ResourceWatcher method has returned.
// Failure to call done will prevent the xDS client from providing future
// ResourceWatcher notifications.
//
// Implementations must embed UnimplementedResourceWatcher.
type ResourceWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resourceData ResourceData, done func())

	// ResourceError indicates an error occurred while trying to fetch or
	// decode the associated resource. The previous version of the resource
	// should be considered invalid.
	ResourceError(err error, done func())

	// AmbientError indicates an error occurred after a resource has been
	// received that should not modify the use of that resource but may provide
	// useful information about the state of the XDSClient for debugging
	// purposes. The previous version of the resource should still be
	// considered valid.
	AmbientError(err error, done func())

	mustEmbedUnimplementedResourceWatcher()
}

// UnimplementedResourceWatcher must be embedded to have forward compatible
// implementations of ResourceWatcher.
type UnimplementedResourceWatcher struct{}

// ResourceChanged is a no-op implementation of
// ResourceWatcher.ResourceChanged that invokes done if non-nil.
func (UnimplementedResourceWatcher) ResourceChanged(_ ResourceData, done func()) {
	if done != nil {
		done()
	}
}

// ResourceError is a no-op implementation of ResourceWatcher.ResourceError
// that invokes done if non-nil.
func (UnimplementedResourceWatcher) ResourceError(_ error, done func()) {
	if done != nil {
		done()
	}
}

// AmbientError is a no-op implementation of ResourceWatcher.AmbientError that
// invokes done if non-nil.
func (UnimplementedResourceWatcher) AmbientError(_ error, done func()) {
	if done != nil {
		done()
	}
}

func (UnimplementedResourceWatcher) mustEmbedUnimplementedResourceWatcher() {}
