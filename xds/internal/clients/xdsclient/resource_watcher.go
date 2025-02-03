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

// OnUpdateProcessed is a function to be invoked by resource watcher
// implementations upon completing the processing of a callback from the xDS
// client. Failure to invoke this callback prevents the xDS client from reading
// further messages from the xDS server.
type OnUpdateProcessed func()

// ResourceDataOrError is a struct that contains either [ResourceData] or
// error. It is used to represent the result of an xDS resource update. Exactly
// one of Data or Err will be non-nil.
type ResourceDataOrError struct {
	Data ResourceData
	Err  error
}

// ResourceWatcher wraps the callbacks to be invoked for different events
// corresponding to the resource being watched. gRFC A88 contains an exhaustive
// list of what method is invoked under what conditions.
type ResourceWatcher interface {
	// OnResourceChanged is invoked to notify the watcher of a new version of
	// the resource received from the xDS server or an error indicating the
	// reason why the resource cannot be obtained.
	//
	// Upon receiving this, in case of an error, the watcher should
	// stop using any previously seen resource. The xDS client will remove the
	// resource from its cache.
	OnResourceChanged(ResourceDataOrError, OnUpdateProcessed)

	// OnAmbientError is invoked if resource is already cached under different
	// error conditions.
	//
	// Upon receiving this, the watcher may continue using the previously seen
	// resource. The xDS client will not remove the resource from its cache.
	OnAmbientError(error, OnUpdateProcessed)
}
