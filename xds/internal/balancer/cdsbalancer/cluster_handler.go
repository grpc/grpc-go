/*
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

package cdsbalancer

import (
	"errors"
	"sync"

	xdsclient "google.golang.org/grpc/xds/internal/client"
)

var errNotReceivedUpdate = errors.New("tried to construct a cluster update on a cluster that has not received an update")

// clusterHandler will be given a name representing a cluster. It will then update the CDS policy
// constantly with a list of Clusters to pass down to XdsClusterResolverLoadBalancingPolicyConfig
// in a stream like fashion.
type clusterHandler struct {
	// A mutex to protect entire tree of clusters.
	clusterMutex    sync.Mutex
	root            *clusterNode
	rootClusterName string

	// A way to ping CDS Balancer about any updates or errors to a Node in the tree.
	// This will either get called from this handler constructing an update or from a child with an error.
	// Capacity of one as the only update CDS Balancer cares about is the most recent update.
	updateChannel chan clusterHandlerUpdate

	xdsClient xdsClientInterface
}

func (ch *clusterHandler) updateRootCluster(rootClusterName string) {
	ch.clusterMutex.Lock()
	defer ch.clusterMutex.Unlock()
	if ch.root == nil {
		// Construct a root node on first update.
		ch.root = createClusterNode(rootClusterName, ch.xdsClient, ch)
		ch.rootClusterName = rootClusterName
		return
	}
	// Check if root cluster was changed. If it was, delete old one and start new one, if not
	// do nothing.
	if rootClusterName != ch.rootClusterName {
		ch.root.delete()
		ch.root = createClusterNode(rootClusterName, ch.xdsClient, ch)
		ch.rootClusterName = rootClusterName
	}
}

// This function tries to construct a cluster update to send to CDS.
func (ch *clusterHandler) constructClusterUpdate() {
	// If there was an error received no op, as this simply means one of the children hasn't received an update yet.
	if clusterUpdate, err := ch.root.constructClusterUpdate(); err == nil {
		// For a ClusterUpdate, the only update CDS cares about is the most recent one, so opportunistically drain the
		// update channel before sending the new update.
		select {
		case <-ch.updateChannel:
		default:
		}
		ch.updateChannel <- clusterHandlerUpdate{chu: clusterUpdate, err: nil}
	}
}

// This logically represents a cluster. This handles all the logic for starting and stopping
// a cluster watch, handling any updates, and constructing a list recursively for the ClusterHandler.
type clusterNode struct {
	// A way to cancel the watch for the cluster.
	cancelFunc func()

	// A list of children, as the Node can be an aggregate Cluster.
	children []*clusterNode

	// A ClusterUpdate in order to build a list of cluster updates for CDS to send down to child
	// XdsClusterResolverLoadBalancingPolicy.
	clusterUpdate xdsclient.ClusterUpdate

	// This boolean determines whether this Node has received an update or not. This isn't the best practice,
	// but this will protect a list of Cluster Updates from being constructed if a cluster in the tree has not received
	// an update yet.
	receivedUpdate bool

	clusterHandler *clusterHandler
}

// CreateClusterNode creates a cluster node from a given clusterName. This will also start the watch for that cluster.
func createClusterNode(clusterName string, xdsClient xdsClientInterface, topLevelHandler *clusterHandler) *clusterNode {
	c := &clusterNode{
		clusterHandler: topLevelHandler,
	}
	// Communicate with the xds client here.
	c.cancelFunc = xdsClient.WatchCluster(clusterName, c.handleResp)
	return c
}

// This function cancels the cluster watch on the cluster and all of it's children.
func (c *clusterNode) delete() {
	c.cancelFunc()
	for _, child := range c.children {
		child.delete()
	}
}

// Construct cluster update (potentially a list of ClusterUpdates) for a node.
func (c *clusterNode) constructClusterUpdate() ([]xdsclient.ClusterUpdate, error) {
	// If the cluster has not yet received an update, the cluster update is not yet ready.
	if !c.receivedUpdate {
		return nil, errNotReceivedUpdate
	}

	// Base case - LogicalDNS or EDS. Both of these cluster types will be tied to a single ClusterUpdate.
	if c.clusterUpdate.ClusterType != xdsclient.ClusterTypeAggregate {
		return []xdsclient.ClusterUpdate{c.clusterUpdate}, nil
	}

	// If an aggregate construct a list by recursively calling down to all of it's children.
	var childrenUpdates []xdsclient.ClusterUpdate
	for _, child := range c.children {
		childUpdateList, err := child.constructClusterUpdate()
		if err != nil {
			return nil, err
		}
		childrenUpdates = append(childrenUpdates, childUpdateList...)
	}
	return childrenUpdates, nil
}

// deltaInClusterUpdate determines whether there was a delta in the cluster update "fields" (meaning irrespective of children)
// received from the xdsclient vs. the clusterUpdate that was currently present for a given cluster node. This will be used to
// help decide whether to ping the cluster handler at the end of the handleResp() callback or not.
func deltaInClusterUpdateFields(clusterUpdateReceived xdsclient.ClusterUpdate, clusterUpdateCurrent xdsclient.ClusterUpdate) bool {
	if clusterUpdateReceived.ServiceName != clusterUpdateCurrent.ServiceName {
		return true
	}
	if clusterUpdateReceived.EnableLRS != clusterUpdateCurrent.EnableLRS {
		return true
	}
	if clusterUpdateReceived.ClusterType != clusterUpdateCurrent.ClusterType {
		return true
	}
	if clusterUpdateReceived.SecurityCfg != clusterUpdateCurrent.SecurityCfg {
		return true
	}
	if clusterUpdateReceived.MaxRequests != clusterUpdateCurrent.MaxRequests {
		return true
	}
	return false
}

// handleResp handles a xds response for a particular cluster. This function also handles any logic with regards to any
// child state that may have changed. At the end of the handleResp(), the clusterUpdate will be pinged in certain situations
// to try and construct an update to send back to CDS.
func (c *clusterNode) handleResp(clusterUpdate xdsclient.ClusterUpdate, err error) {
	c.clusterHandler.clusterMutex.Lock()
	defer c.clusterHandler.clusterMutex.Unlock()
	if err != nil { // Write this error for run() to pick up in CDS LB policy.
		// For a ClusterUpdate, the only update CDS cares about is the most recent one, so opportunistically drain the
		// update channel before sending the new update.
		select {
		case <-c.clusterHandler.updateChannel:
		default:
		}
		c.clusterHandler.updateChannel <- clusterHandlerUpdate{chu: nil, err: err}
		return
	}

	// deletedChild helps determine whether this callback will ping the overall clusterHandler to try and construct an
	// update to send back to CDS. This will be determined by whether there would be a change in the overall clusterUpdate
	// for the whole tree (ex. change in clusterUpdate for current cluster or a deleted child) and also if there's even
	// a possibility for the update to build (ex. if a child is created and a watch is started, that child hasn't received
	// an update yet due to the mutex lock on this callback).
	var deletedChild bool
	// deltaInChildUpdateFields determines whether there was a delta in the clusterUpdate fields (forgetting the children).
	// This will be used to help determine whether to pingClusterHandler at the end of this callback or not.
	deltaInChildUpdateFields := deltaInClusterUpdateFields(clusterUpdate, c.clusterUpdate)

	c.receivedUpdate = true
	c.clusterUpdate = clusterUpdate

	// This map will be empty if the cluster update specifies cluster is an EDS or LogicalDNS cluster, as will have no children.
	newChildren := make(map[string]struct{})
	if clusterUpdate.ClusterType == xdsclient.ClusterTypeAggregate {
		for _, childName := range clusterUpdate.PrioritizedClusterNames {
			newChildren[childName] = struct{}{}
		}
	}

	for _, child := range c.children {
		// If the child is still present in the update, then there is nothing to do for that child name in the update.
		if _, found := newChildren[child.clusterUpdate.ServiceName]; found {
			delete(newChildren, child.clusterUpdate.ServiceName)
		} else { // If the child is no longer present in the update, that cluster can be deleted.
			deletedChild = true
			child.delete()
			// There is no need to delete the pointer from the current children list. A new list of pointers will be constructed
			// at the end of this callback to replace the list of pointers.
		}
	}

	// Whatever clusters are left over here from the update are all new children, so create CDS watches for those clusters.
	// The order of the children list matters, so use the clusterUpdate from xdsclient as the ordering, and use that logical
	// ordering for the new children list. This will be a mixture of child nodes already constructed and also children
	// that need to be created.
	var children []*clusterNode

	// For constant accesses in the construction of new children list.
	mapCurrentChildren := make(map[string]*clusterNode)
	if len(clusterUpdate.PrioritizedClusterNames) != 0 {
		for _, child := range c.children {
			mapCurrentChildren[child.clusterUpdate.ServiceName] = child
		}
	}

	for _, orderedChild := range clusterUpdate.PrioritizedClusterNames {
		// If the child is in the newChildren map, that means that that child must be created. If not, you can just pull
		// the pointer off the current children list (which was mapped for constant accesses).
		if _, toBeCreated := newChildren[orderedChild]; toBeCreated {
			children = append(children, createClusterNode(orderedChild, c.clusterHandler.xdsClient, c.clusterHandler))
		} else { // The child already exists in memory and has already had a watch started for it.
			currentChild, _ := mapCurrentChildren[orderedChild]
			children = append(children, currentChild)
		}
	}

	c.children = children

	// If the cluster is an aggregate cluster, if this callback created any new child cluster nodes, then there's no
	// possibility for a full cluster update to successfully build, as those created children will not have received
	// an update yet. However, if there was simply a child deleted, then there is a possibility that it will have a full
	// cluster update to build and also will have a changed overall cluster update from the deleted child.
	if clusterUpdate.ClusterType == xdsclient.ClusterTypeAggregate {
		if deletedChild && len(newChildren) == 0 {
			c.clusterHandler.constructClusterUpdate()
			return
		}
	}

	// If the cluster was a leaf node, if the cluster update received had change in the cluster update then the overall
	// cluster update would change and there is a possibility for the overall update to build so ping cluster handler to
	// return.
	if deltaInChildUpdateFields {
		c.clusterHandler.constructClusterUpdate()
	}
}
