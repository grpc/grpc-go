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
 *
 */

package clusterresolver

import (
	"google.golang.org/grpc/xds/internal/xdsclient"
)

// watchUpdate wraps the information received from a registered EDS watcher. A
// non-nil error is propagated to the underlying child balancer. A valid update
// results in creating a new child balancer (priority balancer, if one doesn't
// already exist) and pushing the updated balancer config to it.
type watchUpdate struct {
	eds xdsclient.EndpointsUpdate
	err error
}

// edsWatcher takes an EDS balancer config, and use the xds_client to watch EDS
// updates. The EDS updates are passed back to the balancer via a channel.
type edsWatcher struct {
	parent *clusterResolverBalancer

	updateChannel chan *watchUpdate

	edsToWatch string
	edsCancel  func()
}

func (ew *edsWatcher) updateConfig(config *EDSConfig) {
	// If EDSServiceName is set, use it to watch EDS. Otherwise, use the cluster
	// name.
	newEDSToWatch := config.EDSServiceName
	if newEDSToWatch == "" {
		newEDSToWatch = config.ClusterName
	}

	if ew.edsToWatch == newEDSToWatch {
		return
	}

	// Restart EDS watch when the eds name to watch has changed.
	ew.edsToWatch = newEDSToWatch

	if ew.edsCancel != nil {
		ew.edsCancel()
	}
	cancelEDSWatch := ew.parent.xdsClient.WatchEndpoints(newEDSToWatch, func(update xdsclient.EndpointsUpdate, err error) {
		select {
		case <-ew.updateChannel:
		default:
		}
		ew.updateChannel <- &watchUpdate{eds: update, err: err}
	})
	ew.parent.logger.Infof("Watch started on resource name %v with xds-client %p", newEDSToWatch, ew.parent.xdsClient)
	ew.edsCancel = func() {
		cancelEDSWatch()
		ew.parent.logger.Infof("Watch cancelled on resource name %v with xds-client %p", newEDSToWatch, ew.parent.xdsClient)
	}

}

// stopWatch stops the EDS watch.
//
// Call to updateConfig will restart the watch with the new name.
func (ew *edsWatcher) stopWatch() {
	if ew.edsCancel != nil {
		ew.edsCancel()
		ew.edsCancel = nil
	}
	ew.edsToWatch = ""
}
