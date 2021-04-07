package cdsbalancer // Think of a package like a logical circle in my diagram
import (
	xdsclient "google.golang.org/grpc/xds/internal/client"
	"sync"
)

// What does it need to import? I think this happens as a result of what you define as your code etc.

// Do I construct this conceptually first? I think so. Constructing it conceptually will give me a logical framework before any implementation.
// Also, constructing it conceptually will give me a framework of day by day and organization. I need to be able to quickly plug in and do work,
// without having to context switch between components.

// <- conceptual

// Object: ClusterHandler - ClusterHandler will be given a name representing a cluster. It will then update the CDS policy
// constantly with a list of Clusters to pass down in a stream like fashion.
type ClusterHandler struct { // <- Implementation
	// What state does this object need? - a pointer to root
	root *ClusterNode
	// A way to communicate with clusterHandler in a stream like way

	// A way to get pinged about any updates to a child Node
	updateChannel chan string // Will get constructed in a struct literal in cds balancer
	// A mutex to protect entire tree
	clusterMutex sync.Mutex
	// A channel to communicate with the cds balancer any updates
	cdsUpdate chan<- []xdsclient.ClusterUpdate
}

// External API:
// UpdateRootCluster - This external API will simply take a clusterName and update the root node.
func (ch *ClusterHandler) updateRootCluster(clusterName string) /*Return value - the expectation for a stream back to the CDS Policy*/ {
	// Perhaps here check if root node was changed. If it was, delete old one and start new one, if not
	// do nothing.
	if ch.root != nil || clusterName != ch.root.clusterUpdate.ServiceName {
		ch.root = CreateClusterNode(clusterName, ch.clusterMutex, ch.updateChannel)
	}
}
// Method

// Message passed into it

// Message returned
// Method: watcher - this watches for updates on any ClusterNode comprising the tree, and sends out a response to cds balancer
func (ch *ClusterHandler) watcher() {
	// Wait for an update from a child node
	// Send out a response to cds balancer with the new updated list (by calling root node)
	for {
		select{
		case <-ch.updateChannel:
			// Also needs a wait on a context?
		}

		// Get an updated list here, have it ping cds policy hey I got a new list
		// Send it a list of cluster updates and then do conversion in cds balancer.
		ch.clusterMutex.Lock()
		// On an error case, simply just ignore the constructed update list.
		if clusterUpdate, err := ch.root.constructClusterUpdate(); err != nil {
			ch.cdsUpdate <- clusterUpdate
		}
		ch.clusterMutex.Unlock()
	}
}

// Internal API:

// Method

// Message passed into it

// Message returned

// Object: ClusterNode - This object represents a cluster. This handles all the logic for starting and stopping
// the watch, handling any updates
type ClusterNode struct { // <- Implementation
	// What state does this object need?

	// A way to cancel the watch for the cluster.
	cancelFunc func()

	// A list of children, as the Node can be an aggregate Cluster.
	children []*ClusterNode
	// A ClusterUpdate in order to build a list of cluster updates to send down to child Policy.
	clusterUpdate xdsclient.ClusterUpdate

	// A way to communicate with cluster handler about a cluster update, as any node can be updated at any time.
	clusterMutex sync.Mutex
	updateChannel chan string

	// This boolean determines whether this Node has received the first update or not. This isn't the best practice,
	// but this will protect a Cluster Update OR MAKE THIS ON A NIL CHECK
	receivedFirstUpdate bool
}
// External API:
// Method - CreateClusterNode - This method creates a cluster node from a given string. This will start the watch
// (communication with the xds client.)
func CreateClusterNode(clusterName string, clusterMutex sync.Mutex, updateChannel chan string) *ClusterNode {
	c := &ClusterNode{
		clusterMutex: clusterMutex,
		updateChannel: updateChannel,
	}
	// Communicate with the xds client here.
	c.cancelFunc = xdsClient.WatchCluster(clusterName, c.handleResp)
	/*
	func() {
	lock handle resp unlock
	*/
	return c
}

// Internal API:
// Method - delete - This object cancels the watch for it's cluster and all of it's children.
func (c *ClusterNode) delete() {
	c.clusterMutex.Lock()
	c.cancelFunc()
	for _, child := range c.children {
		child.delete()
	}
	c.clusterMutex.Unlock()
}
// CHANGE THIS TO A GLOBAL MUTEX HERE
/*

*/
// Have another return value on an error, just have it not send anything in the ClusterHandler back to the CDS
// Method: constructClusterUpdate - Construct cluster update (a list of ClusterUpdates) for a node.
// This will be called recursively, and the base case will be on a LogicalDNS or an EDS cluster
func (c *ClusterNode) constructClusterUpdate() ([]xdsclient.ClusterUpdate, error) {
	// Base case - LogicalDNS or EDS
	if c.clusterUpdate.ClusterType != xdsclient.ClusterTypeAggregate {
		return []xdsclient.ClusterUpdate{c.clusterUpdate}, nil
	}
	// If an aggregate loop through all children and then
	// append constructClusterUpdate of the children to everything.
	var childrenUpdates []xdsclient.ClusterUpdate
	for _, child := range c.children {
		childUpdateList, err := child.constructClusterUpdate()
		if err != nil {
			return nil, err
		}
		for _, clusterUpdate := range childUpdateList { // will be recursively
			childrenUpdates = append(childrenUpdates, clusterUpdate)
		}
	}
	// ABORT CLAUSE HERE IF NOT UPDATED or hasn't received an update
	return childrenUpdates, nil
}

// Method: handleResp - HandleResponse handles a xds response for a particular cluster. This function also
// handles any logic with regards to any child state that may have changed.
func (c *ClusterNode) handleResp(clusterUpdate xdsclient.ClusterUpdate, err error) /*Changes things internally, so doesn't return anything*/ {
	c.clusterMutex.Lock()
	c.receivedFirstUpdate = true
	// What do I do on an error condition?...
	// Make this happen atomically, and afterward ping root handler to send a stream back to cds balancer
	c.clusterUpdate = clusterUpdate

	// This variable determines whether there was a delta with regards to this clusterupdate. If there was, ping
	// update handler.
	var delta bool

	// First thing you do up here is get the cluster update, and construct a map
	newChildren := make(map[string]struct{})
	// This map will be empty if type EDS or LogicalDNS, as will have no children.
	if clusterUpdate.ClusterType == xdsclient.ClusterTypeAggregate {
		// Iterate through ClusterUpdate here and construct a map
		for _, childName := range clusterUpdate.PrioritizedClusterNames {
			newChildren[childName] = struct{}{}
		}
	}
	// Iterate through the children names, which should be c.children.ClusterUpdate.
	for _, child := range(c.children) {
		// If the child is still in the update, then sweet
		if _, found := newChildren[child.clusterUpdate.ServiceName]; found {
			// Delete that child from map, as that child is already in this map
			delete(newChildren, child.clusterUpdate.ServiceName)
		} else { // If the child is no longer in the update, then that means the child is deleted
			// Delete the child, this will introduce concurrency issues you then need to deal with
			child.delete()
		}
	}

	// whatever is left over here start watches for, which will happen implicitly through the creation of the new node
	for child, _ := range newChildren {
		c.children = append(c.children, CreateClusterNode(child, c.clusterMutex, c.updateChannel)) // Not a recursive mutex lock
	}
	// Figure out when to tell the handler that it has a new update.
	if delta {
		c.updateChannel<-"There was an update"
	}
	c.clusterMutex.Unlock() // You now have a guarantee from memory model that this unlock comes before the next .Lock() call
}

