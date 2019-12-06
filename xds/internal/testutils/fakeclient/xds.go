/*
 *
 * Copyright 2019 gRPC authors.
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

// Package fakeclient provides a fake implmementation of the xds client, for
// use in unittests.
package fakeclient

import (
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"google.golang.org/grpc/xds/internal/testutils/channel"
)

// XDS is a fake implementation of the xds client. It exposes a bunch of
// channels to signal the occurrence of various events.
type XDS struct {
	serviceCb  func(xdsclient.ServiceUpdate, error)
	suWatchCh  *channel.WithTimeout
	closeCh    *channel.WithTimeout
	suCancelCh *channel.WithTimeout
}

// WatchService registers a LDS/RDS watch.
func (xdsC *XDS) WatchService(target string, callback func(xdsclient.ServiceUpdate, error)) func() {
	xdsC.serviceCb = callback
	xdsC.suWatchCh.Send(target)
	return func() {
		xdsC.suCancelCh.Send(nil)
	}
}

// WaitForWatchService waits for WatchService to be invoked on this client
// within a reasonable timeout.
func (xdsC *XDS) WaitForWatchService() (string, error) {
	val, err := xdsC.suWatchCh.Receive()
	return val.(string), err
}

// InvokeWatchServiceCb invokes the registered service watch callback.
func (xdsC *XDS) InvokeWatchServiceCb(cluster string, err error) {
	xdsC.serviceCb(xdsclient.ServiceUpdate{Cluster: cluster}, err)
}

// Close closes the xds client.
func (xdsC *XDS) Close() {
	xdsC.closeCh.Send(nil)
}

// NewXDS returns a new fake xds client.
func NewXDS() *XDS {
	return &XDS{
		suWatchCh:  channel.NewChanWithTimeout(),
		closeCh:    channel.NewChanWithTimeout(),
		suCancelCh: channel.NewChanWithTimeout(),
	}
}
