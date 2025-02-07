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

// OnCallbackProcessed is a function to be invoked by resource watcher
// implementations upon completing the processing of a callback from the xDS
// client. Failure to invoke this callback prevents the xDS client from reading
// further messages from the xDS server.
type OnCallbackProcessed func()

// ResourceDataOrError is a struct that contains either ResourceData or
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
	// ResourceChanged either indicates a new version of the resource is
	// available or an an error occurred while trying to fetch or decode the
	// associated resource. In case of an error, the previous version of the
	// resource should be considered invalid.
	ResourceChanged(ResourceDataOrError, OnCallbackProcessed)

	// AmbientError indicates an error occurred while trying to fetch or decode
	// the associated resource.  The previous version of the resource should still
	// be considered valid.
	AmbientError(err error, done func())
}
