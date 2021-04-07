package cdsbalancer

import xdsclient "google.golang.org/grpc/xds/internal/client"

// A global to mock out in tests? To return certain functions?
var xdsClient xdsClientInterface

// Global mutex here? This should allow you to protect whole tree?

// Type ClusterHandler is a wrapper around the tree of cluster(s) that represent the cluster(s)
// being watched for in cdsbalancer. This serves as the interface for how cds balancer turns a
// cluster name into a list of resource names to pass down to xds_cluster_resolver.
// HAVE THE CLUSTER HANDLER SEND BACK SOMETHING, BUT HAVE THE CDS BALANCER ACTUALLY CREATE THE EDS BALANCER FROM WHAT YOU PASS IT FROM HERE
// BLACK BOX ONTO THIS RETURNING A LIST
// HANDLE CLUSTER HANDLER UPDATE
/*type ClusterHandler struct {
	root *ClusterNode
	// Or do I put the variable here?
}
// I think it should be instaintiated on cds build, then expose a client conn update, which will send back a config
// also have a channel for nodes to communicate something changed on an update, then send an updated list back by putting on a buffer
// somewhere or something
// handle ClientConn update

// Checks for if there is a difference between the cluster name received and the cluster name of the root node (initally should be empty)
// If there is a difference, delete node and replace it with new one?

// Logically, this will take in a cluster name and return a list of child policies, every time it gets a new cluster name, or I guess every time update

// Any update in the tree tell the cds balancer there is something different in regards to config
func updateCDSBalancer

func () // Handle Client Conn update from higher level, should do all logic for simply the root Node

// NewClusterHandler receives a clustername from cdslbpolicy, and then constructs a cluster node related to this cluster.
// This constructed cluster will serve as the root cluster, as in the case of Aggregate Clusters there is a potential
// for a cluster to be a tree of clusters.

// Perhaps this callback calls back into cds lb with the correct information that you trivially convert to that struct thing
func newClusterHandler(clusterName string, callback func()) *ClusterHandler { // Is this callback used for communication with cds? Seems like this should be handleWatchUpdate.
	return &ClusterHandler{
		root: createClusterNode(clusterName),
	}
	client, err := newXDSClient()
	if err != nil {
		//b.logger.Errorf("failed to create xds-client: %v", err)
		return nil // Do I return an error here?
	}
	xdsClient = client
	defer func() {
		xdsClient = nil
	}()
	// Defer setting to nil
}
// Perhaps send back a list of cluster updates, and have cds handle it
// have a function here that starts at the root and constructs an update to send back to Cds policy
func constructClusterUpdate() {
	// Apparently, you use a slice here instead of a list
	var clusterUpdateList []xdsclient.ClusterUpdate

	// Base case of tree traversal: a logical dns or aggregate cluster
	// Iterative recursion here
}

// How do we pass xdsClientInterface around, we could have it as a global?
// I kind of like the idea of having a cluster update persisted here, and constructing the cluster update from a tree traversal from ClusterHandler
type ClusterNode struct {
	cancelFunc func()
	children []*ClusterNode

	// Map clusterName to a ClusterNode here, for fast lookups to compute a difference

	// Persist a clusterUpdate here for tree traversal to look at
	clusterUpdate xdsclient.ClusterUpdate
}

// CreateClusterNode creates a cluster, and starts a watch with the xds client.

func createClusterNode(clusterName string) *ClusterNode {
	c := &ClusterNode{}
	c.cancelFunc = xdsClient.WatchCluster(clusterName, c.handleResp()) // All the second function does is write to the wire
	return c
}
// MOVE ALL HANDLE LOGIC HERE
// HandleResponse handles a xds response for a particular cluster.
// This function also updates any children state that may have changed (i.e. if the cluster
// was previously an aggregate cluster with children, the children could change, the
// aggregate cluster could change to a dns cluster and lose it's children.)
// Even if this isn't in the right place, will interface with this ClusterUpdate regardless

// This function also computes the way you
// IF INITIALLY EMPTY, WHAT'S NICE ABOUT THIS IS IT WILL STILL WORK AS DELTA OF CHILDREN WOULD BE NICE
func (c *ClusterNode) handleResp(clusterUpdate xdsclient.ClusterUpdate) {
	// Persist a clusterUpdate for tree traversal to look at
	c.clusterUpdate = clusterUpdate
	// It seems like for this, you want to compute the delta in children, then update and cancel accordingly

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
		child.clusterUpdate.ServiceName

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
		c.children = append(c.children, createClusterNode(child))
	}
	// Calculate the map difference, and then afterward call into delete() or createClusterNode()
	// on the different nodes deleted or created
	// Construct two maps here. One map representing the response of the clusters, and one map already there in
	// memory. Or, simply iterate through the list, where if it's not in the map

	// Abstract algorithm that I developed I put here

}

// Delete cancels the watch for the Cluster and also cancels the watch of all the Cluster's children.
func (c *ClusterNode) delete() {
	// TODO: any other logic...mutex locks for sure
	c.cancelFunc()
	for _, child := range c.children {
		child.delete()
	}
}
// A cluster update for a the node. The base case will be a logical dns or eds cluster, recursively call downward if aggregate
func (c *ClusterNode) constructClusterUpdate {

}