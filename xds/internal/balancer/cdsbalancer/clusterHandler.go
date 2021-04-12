package cdsbalancer
import (
	"errors"
	"strings"
	"sync"

	xdsclient "google.golang.org/grpc/xds/internal/client"
)


// ClusterHandler will be given a name representing a cluster. It will then update the CDS policy
// constantly with a list of Clusters to pass down to XdsClusterResolverLoadBalancingPolicyConfig
// in a stream like fashion.
type ClusterHandler struct {
	root *ClusterNode

	// A way to get pinged about any updates or errors to a Node in the tree.
	updateChannel chan string

	// A mutex to protect entire tree.
	clusterMutex sync.Mutex
	// A channel to communicate with the cds balancer about any updates.
	cdsUpdate chan<- []xdsclient.ClusterUpdate
	// A channel to communicate with the cds balancer about any errors.
	errChan chan error

	xdsClient      xdsClientInterface
}

func (ch *ClusterHandler) updateRootCluster(clusterName string) /*Return value - the expectation for a stream back to the CDS Policy*/ {
	if ch.root == nil {
		// Construct a root node on first update.
		ch.root = CreateClusterNode(clusterName, ch.clusterMutex, ch.updateChannel, ch.xdsClient)
		return
	}
	// Check if root cluster was changed. If it was, delete old one and start new one, if not
	// do nothing.
	if clusterName != ch.root.clusterUpdate.ServiceName {
		ch.root.delete()
		ch.root = CreateClusterNode(clusterName, ch.clusterMutex, ch.updateChannel, ch.xdsClient)
	}
}

// Watches for updates on any ClusterNode comprising the tree, and sends out a response to cds balancer.
// Will get run from cds.
func (ch *ClusterHandler) watcher() {
	for {
		select{
		case update := <-ch.updateChannel:
			if strings.Contains(update, "error") {
				ch.errChan<-errors.New("There was an error in a child")
			}

			// Send CDS back a list of cluster updates which will be forwarded to the xds_cluster_resolver LB policy.
			ch.clusterMutex.Lock()
			// On the error case here, one of the nodes in the tree has not yet received it's first update. Thus,
			// simply ignore this updateChannel ping.
			if clusterUpdate, err := ch.root.constructClusterUpdate(); err != nil {
				ch.cdsUpdate <- clusterUpdate
			}
			ch.clusterMutex.Unlock()
		// TODO: a wait on a context?
		}
	}
}

// This logically represents a cluster. This handles all the logic for starting and stopping
// a cluster watch, handling any updates, and constructing a list recursively for the ClusterHandler.
type ClusterNode struct { // <- Implementation
	// A way to cancel the watch for the cluster.
	cancelFunc func()

	// A list of children, as the Node can be an aggregate Cluster.
	children []*ClusterNode
	// A ClusterUpdate in order to build a list of cluster updates for CDS to send down to child
	// XdsClusterResolverLoadBalancingPolicy.
	clusterUpdate xdsclient.ClusterUpdate
	// Global mutex.
	clusterMutex sync.Mutex

	// A way to communicate with the cluster handler about a cluster update, as the cluster can be updated at any time.
	updateChannel chan string

	// This boolean determines whether this Node has received the first update or not. This isn't the best practice,
	// but this will protect a list of Cluster Updates from being constructed if a cluster in the tree has not received
	// an update yet.
	receivedFirstUpdate bool

	xdsClient xdsClientInterface
}

// CreateClusterNode creates a cluster node from a given clusterName. This will also start the watch for that cluster.
func CreateClusterNode(clusterName string, clusterMutex sync.Mutex, updateChannel chan string, xdsClient xdsClientInterface) *ClusterNode {
	c := &ClusterNode{
		clusterMutex: clusterMutex,
		updateChannel: updateChannel,
		xdsClient: xdsClient,
	}
	// Communicate with the xds client here. TODO: Mutex handling here?
	c.cancelFunc = xdsClient.WatchCluster(clusterName, c.handleResp)
	return c
}

// This function serves as a wrapper around recursive logic in order to grab the lock.
func (c *ClusterNode) delete() {
	c.clusterMutex.Lock()
	c.deleteRecursive()
	c.clusterMutex.Unlock()
}

func (c *ClusterNode) deleteRecursive() {
	c.cancelFunc()
	for _, child := range c.children {
		child.delete()
	}
}

// Construct cluster update (potentially a list of ClusterUpdates) for a node.
func (c *ClusterNode) constructClusterUpdate() ([]xdsclient.ClusterUpdate, error) {
	// If the cluster has not yet received an update, the cluster update is not yet ready.
	if !c.receivedFirstUpdate {
		return nil, errors.New("Tried to construct a cluster update on a cluster that has not received an update.")
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
		for _, clusterUpdate := range childUpdateList {
			childrenUpdates = append(childrenUpdates, clusterUpdate)
		}
	}

	return childrenUpdates, nil
}

// handleResp handles a xds response for a particular cluster. This function also
// handles any logic with regards to any child state that may have changed.
func (c *ClusterNode) handleResp(clusterUpdate xdsclient.ClusterUpdate, err error) /*Changes things internally, so doesn't return anything*/ {
	c.clusterMutex.Lock()
	if err != nil {
		c.updateChannel<-"There was an error."
	}
	c.receivedFirstUpdate = true
	c.clusterUpdate = clusterUpdate

	// This variable will determine whether there was a delta with regards to this clusterupdate. If there was, at the end
	// of the response ping ClusterHandler to send CDS Policy a new list of ClusterUpdates.
	var delta bool

	// This map will be empty if the cluster update specifies cluster is an EDS or LogicalDNS cluster, as will have no children.
	newChildren := make(map[string]struct{})
	if clusterUpdate.ClusterType == xdsclient.ClusterTypeAggregate {
		for _, childName := range clusterUpdate.PrioritizedClusterNames {
			newChildren[childName] = struct{}{}
		}
	}

	for _, child := range(c.children) {
		// If the child is still present in the update, then there is nothing to do for that child name in the update.
		if _, found := newChildren[child.clusterUpdate.ServiceName]; found {
			delete(newChildren, child.clusterUpdate.ServiceName)
		} else { // If the child is no longer present in the update, that cluster can be deleted.
			delta = true
			child.delete()
		}
	}

	// Whatever clusters are left over here from the update are all new children, so create CDS watches for those clusters.
	for child, _ := range newChildren {
		delta = true
		c.children = append(c.children, CreateClusterNode(child, c.clusterMutex, c.updateChannel, c.xdsClient))
	}
	// If there was a change in the state of the children, ping the ClusterHandler to construct a new update to send back
	// to CDS.
	if delta {
		c.updateChannel<-"There was an update"
	}
	c.clusterMutex.Unlock()
}

