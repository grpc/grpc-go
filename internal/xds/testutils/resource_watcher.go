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

import "google.golang.org/grpc/internal/xds/xdsclient/xdsresource"

// TestResourceWatcher implements the xdsresource.ResourceWatcher interface,
// used to receive updates on watches registered with the xDS client, when using
// the resource-type agnostic WatchResource API.
//
// Tests can use the channels provided by this type to get access to updates and
// errors sent by the xDS client.
type TestResourceWatcher struct {
	// UpdateCh is the channel on which xDS client updates are delivered.
	UpdateCh chan *xdsresource.ResourceData
	// AmbientErrorCh is the channel on which ambient errors from the xDS
	// client are delivered.
	AmbientErrorCh chan error
	// ResourceErrorCh is the channel on which resource errors from the xDS
	// client are delivered.
	ResourceErrorCh chan struct{}
}

// ResourceChanged is invoked by the xDS client to report the latest update.
func (w *TestResourceWatcher) ResourceChanged(data xdsresource.ResourceData, onDone func()) {
	defer onDone()
	select {
	case <-w.UpdateCh:
	default:
	}
	w.UpdateCh <- &data

}

// ResourceError is invoked by the xDS client to report the latest error to
// stop watching the resource.
func (w *TestResourceWatcher) ResourceError(err error, onDone func()) {
	defer onDone()
	select {
	case <-w.ResourceErrorCh:
	case <-w.AmbientErrorCh:
	default:
	}
	w.AmbientErrorCh <- err
	w.ResourceErrorCh <- struct{}{}
}

// AmbientError is invoked by the xDS client to report the latest ambient
// error.
func (w *TestResourceWatcher) AmbientError(err error, onDone func()) {
	defer onDone()
	select {
	case <-w.AmbientErrorCh:
	default:
	}
	w.AmbientErrorCh <- err
}

// NewTestResourceWatcher returns a TestResourceWatcher to watch for resources
// via the xDS client.
func NewTestResourceWatcher() *TestResourceWatcher {
	return &TestResourceWatcher{
		UpdateCh:        make(chan *xdsresource.ResourceData, 1),
		AmbientErrorCh:  make(chan error, 1),
		ResourceErrorCh: make(chan struct{}, 1),
	}
}
