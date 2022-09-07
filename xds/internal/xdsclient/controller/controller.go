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

// Package controller contains implementation to connect to the control plane.
// Including starting the ClientConn, starting the xDS stream, and
// sending/receiving messages.
//
// All the messages are parsed by the resource package (e.g.
// UnmarshalListener()) and sent to the Pubsub watchers.
package controller

import (
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/xds/internal/xdsclient/bootstrap"
	"google.golang.org/grpc/xds/internal/xdsclient/pubsub"
	"google.golang.org/grpc/xds/internal/xdsclient/transport"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

// Controller manages the connection and stream to the control plane.
//
// It keeps track of what resources are being watched, and send new requests
// when new watches are added.
//
// It takes a pubsub (as an interface) as input. When a response is received,
// it's parsed, and the updates are sent to the pubsub.
type Controller struct {
	config          *bootstrap.ServerConfig
	updateHandler   pubsub.UpdateHandler
	updateValidator xdsresource.UpdateValidatorFunc
	logger          *grpclog.PrefixLogger
	transport       *transport.Transport

	mu sync.Mutex
	// Message specific watch infos, protected by the above mutex. These are
	// written to, after successfully reading from the update channel, and are
	// read from when recovering from a broken stream to resend the xDS
	// messages. When the user of this client object cancels a watch call,
	// these are set to nil. All accesses to the map protected and any value
	// inside the map should be protected with the above mutex.
	watchMap map[xdsresource.ResourceType]map[string]bool
}

// New creates a new controller.
func New(config *bootstrap.ServerConfig, updateHandler pubsub.UpdateHandler, validator xdsresource.UpdateValidatorFunc, logger *grpclog.PrefixLogger, boff func(int) time.Duration) (*Controller, error) {
	c := &Controller{
		config:          config,
		updateValidator: validator,
		updateHandler:   updateHandler,
		watchMap:        make(map[xdsresource.ResourceType]map[string]bool),
	}
	if boff == nil {
		boff = backoff.DefaultExponential.Backoff
	}
	tr, err := transport.New(&transport.Options{
		ServerCfg:          config,
		UpdateHandler:      c.handleResourceUpdate,
		StreamErrorHandler: updateHandler.NewConnectionError,
		Backoff:            boff,
		Logger:             logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create a new controller: %v", err)
	}

	c.transport = tr
	return c, nil
}

func (t *Controller) handleResourceUpdate(update transport.ResourceUpdate) error {
	opts := &xdsresource.UnmarshalOptions{
		Version:         update.Version,
		Resources:       update.Resources,
		Logger:          t.logger,
		UpdateValidator: t.updateValidator,
	}
	var md xdsresource.UpdateMetadata
	var err error
	switch {
	case xdsresource.IsListenerResource(update.URL):
		var l map[string]xdsresource.ListenerUpdateErrTuple
		l, md, err = xdsresource.UnmarshalListener(opts)
		t.updateHandler.NewListeners(l, md)
	case xdsresource.IsRouteConfigResource(update.URL):
		var r map[string]xdsresource.RouteConfigUpdateErrTuple
		r, md, err = xdsresource.UnmarshalRouteConfig(opts)
		t.updateHandler.NewRouteConfigs(r, md)
	case xdsresource.IsClusterResource(update.URL):
		var c map[string]xdsresource.ClusterUpdateErrTuple
		c, md, err = xdsresource.UnmarshalCluster(opts)
		t.updateHandler.NewClusters(c, md)
	case xdsresource.IsEndpointsResource(update.URL):
		var e map[string]xdsresource.EndpointsUpdateErrTuple
		e, md, err = xdsresource.UnmarshalEndpoints(opts)
		t.updateHandler.NewEndpoints(e, md)
	default:
		return xdsresource.ErrResourceTypeUnsupported{
			ErrStr: fmt.Sprintf("Resource URL %v unknown in response from server", update.URL),
		}
	}
	return err
}

// Close closes the controller.
func (t *Controller) Close() {
	t.transport.Close()
}
