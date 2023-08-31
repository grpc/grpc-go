/*
 *
 * Copyright 2020 gRPC authors.
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

package resolver

import (
	"context"

	"google.golang.org/grpc/internal/pretty"
	"google.golang.org/grpc/xds/internal/xdsclient/xdsresource"
)

type listenerWatcher struct {
	name   string
	cancel func()
	parent *xdsResolver
}

func newListenerWatcher(name string, parent *xdsResolver) *listenerWatcher {
	lw := &listenerWatcher{name: name, parent: parent}
	lw.cancel = xdsresource.WatchListener(parent.xdsClient, name, lw)
	return lw
}

func (l *listenerWatcher) OnUpdate(update *xdsresource.ListenerResourceData) {
	l.parent.logger.Infof("Received update for Listener resource %q: %v", l.name, pretty.ToJSON(update))

	l.parent.serializer.Schedule(func(context.Context) {
		l.parent.onListenerResourceUpdate(update.Resource)
	})
}

func (l *listenerWatcher) OnError(err error) {
	l.parent.logger.Infof("Received error for Listener resource %q: %v", l.name, err)

	l.parent.serializer.Schedule(func(context.Context) {
		l.parent.onError(err)
	})
}

func (l *listenerWatcher) OnResourceDoesNotExist() {
	l.parent.logger.Infof("Listener resource %q does not exist", l.name)

	l.parent.serializer.Schedule(func(context.Context) {
		l.parent.onListenerResourceNotFound()
	})
}

func (l *listenerWatcher) stop() {
	l.cancel()
	l.parent.logger.Infof("Canceling watch on Listener resource %q", l.name)
}

func newRouteConfigWatcher(name string, parent *xdsResolver) *routeConfigWatcher {
	rw := &routeConfigWatcher{name: name, parent: parent}
	rw.cancel = xdsresource.WatchRouteConfig(parent.xdsClient, name, rw)
	return rw
}

type routeConfigWatcher struct {
	name   string
	cancel func()
	parent *xdsResolver
}

func (r *routeConfigWatcher) OnUpdate(update *xdsresource.RouteConfigResourceData) {
	r.parent.logger.Infof("Received update for RouteConfiguration resource %q: %v", r.name, pretty.ToJSON(update))

	r.parent.serializer.Schedule(func(context.Context) {
		r.parent.onRouteConfigResourceUpdate(r.name, update.Resource)
	})
}

func (r *routeConfigWatcher) OnError(err error) {
	r.parent.logger.Infof("Received error for RouteConfiguration resource %q: %v", r.name, err)

	r.parent.serializer.Schedule(func(context.Context) {
		r.parent.onError(err)
	})
}

func (r *routeConfigWatcher) OnResourceDoesNotExist() {
	r.parent.logger.Infof("RouteConfiguration resource %q does not exist", r.name)

	r.parent.serializer.Schedule(func(context.Context) {
		r.parent.onRouteConfigResourceNotFound(r.name)
	})
}

func (r *routeConfigWatcher) stop() {
	r.cancel()
	r.parent.logger.Infof("Canceling watch on RouteConfiguration resource %q", r.name)
}
