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

// ResourceWatcher wraps the callbacks to be invoked for different events
// corresponding to the resource being watched. gRFC A88 contains an exhaustive
// list of what method is invoked under what conditions.
//
// onCallbackProcessed in the callbacks is a function to be invoked by
// resource watcher implementations upon completing the processing of a
// callback from the xDS client. Failure to invoke this callback prevents the
// xDS client from reading further messages from the xDS server.
type ResourceWatcher interface {
	// ResourceChanged indicates a new version of the resource is available.
	ResourceChanged(resourceData ResourceData, onCallbackProcessed func())

	// ResourceError indicates an error occurred while trying to fetch or
	// decode the associated resource. The previous version of the resource
	// should be considered invalid.
	ResourceError(err error, onCallbackProcessed func())

	// AmbientError indicates an error occurred after a resource has been
	// received that should not modify the use of that resource but may be
	// provide useful information about the ambient state of the XDSClient for
	// debugging purposes. The previous version of the resource should still be
	// considered valid.
	AmbientError(err error, onCallbackProcessed func())
}
