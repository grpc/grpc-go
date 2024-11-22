/*
 *
 * Copyright 2024 gRPC authors.
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

// OnResourceProcessed() is a function to be invoked by watcher implementations upon
// completing the processing of a callback from the xDS client. Failure to
// invoke this callback prevents the xDS client from reading further messages
// from the xDS server.
type OnResourceProcessed func()

// ResourceWatcher is an interface that can to be implemented
// to wrap the callbacks to be invoked for different events corresponding
// to the resource being watched.
type ResourceWatcher interface {
	// OnUpdate is invoked to report an update for the resource being watched.
	// The ResourceData parameter needs to be type asserted to the appropriate
	// type for the resource being watched.
	OnUpdate(ResourceData, OnResourceProcessed)

	// OnError is invoked under different error conditions including but not
	// limited to the following:
	//      - authority mentioned in the resource is not found
	//      - resource name parsing error
	//      - resource deserialization error
	//      - resource validation error
	//      - ADS stream failure
	//      - connection failure
	OnError(error, OnResourceProcessed)

	// OnResourceDoesNotExist is invoked for a specific error condition where
	// the requested resource is not found on the xDS management server.
	OnResourceDoesNotExist(OnResourceProcessed)
}
