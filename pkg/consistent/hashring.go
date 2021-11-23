package consistent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrMemberAlreadyExists = errors.New("member node already exists")
	ErrMemberNotFound      = errors.New("member node not found")
	ErrNotEnoughMembers    = errors.New("not enough member nodes to satisfy request")
)

// HasherFunc is the interface for any function that can act as a hasher.
type HasherFunc func([]byte) uint64

// Member is the interface for any object that can be stored and retrieved as a
// Hashring member (e.g. node/backend).
type Member interface {
	Key() string
}

// Hashring provides a ring consistent hash implementation using a configurable number of virtual
// nodes. It is internally synchronized and thread-safe.
type Hashring struct {
	hasher            HasherFunc
	replicationFactor uint16

	sync.RWMutex
	nodes        map[string]nodeRecord
	virtualNodes virtualNodeList
}

// NewHashring creates a new Hashring with the specified hasher function and replicationFactor.
//
// replicationFactor must be > 0 and should be a number like 20 for higher quality key distribution.
// At a replicationFactor of 100, the standard distribution of key->member mapping will be about 10%
// of the mean. At a replicationFactor of 1000 it will be about 3.2%. The replicationFactor should
// be chosen carefully because a higher replicationFactor will require more memory and worse member
// selection performance.
func NewHashring(hasher HasherFunc, replicationFactor uint16) *Hashring {
	if replicationFactor < 1 {
		panic("replicationFactor must be at least 1")
	}

	return &Hashring{
		hasher:            hasher,
		replicationFactor: replicationFactor,
		nodes:             map[string]nodeRecord{},
	}
}

// Add adds an object that implements the Member interface as a node in the
// consistent hashring.
//
// If a member with the same key is already in the hashring,
// ErrMemberAlreadyExists is returned.
func (h *Hashring) Add(member Member) error {
	nodeKeyString := member.Key()

	h.Lock()
	defer h.Unlock()

	if _, ok := h.nodes[nodeKeyString]; ok {
		// already have node, bail
		return ErrMemberAlreadyExists
	}

	nodeHash := h.hasher([]byte(nodeKeyString))

	newNodeRecord := nodeRecord{
		nodeHash,
		nodeKeyString,
		member,
		nil,
	}

	// virtualNodeBuffer is a 10-byte array, where 8 bytes are the hash value of
	// the member key, and the final 2 bytes are an offset of the virtual node
	// itself. This value is then hashed to get the final hash value of the virtual node.
	virtualNodeBuffer := make([]byte, 10)
	binary.LittleEndian.PutUint64(virtualNodeBuffer, nodeHash)

	for i := uint16(0); i < h.replicationFactor; i++ {
		binary.LittleEndian.PutUint16(virtualNodeBuffer[8:], i)
		virtualNodeHash := h.hasher(virtualNodeBuffer)

		virtualNode := virtualNode{
			virtualNodeHash,
			newNodeRecord,
		}

		newNodeRecord.virtualNodes = append(newNodeRecord.virtualNodes, virtualNode)
		h.virtualNodes = append(h.virtualNodes, virtualNode)
	}

	sort.Sort(h.virtualNodes)

	// Add the node to our map of nodes
	h.nodes[nodeKeyString] = newNodeRecord

	return nil
}

// Remove removes an object with the same key as the specified member object.
//
// If no member with the same key is in the hashring, ErrMemberNotFound is returned.
func (h *Hashring) Remove(member Member) error {
	nodeKeyString := member.Key()

	h.Lock()
	defer h.Unlock()

	foundNode, ok := h.nodes[nodeKeyString]
	if !ok {
		// don't have the node, bail
		return ErrMemberNotFound
	}

	indexesToRemove := make([]int, 0, h.replicationFactor)
	for _, vnode := range foundNode.virtualNodes {
		vnodeIndex := sort.Search(len(h.virtualNodes), func(i int) bool {
			return !h.virtualNodes[i].less(vnode)
		})
		if vnodeIndex >= len(h.virtualNodes) {
			panic(fmt.Sprintf("unable to find vnode to remove: %020d:%020d:%s", vnode.hashvalue, vnode.members.hashvalue, vnode.members.nodeKey))
		}

		indexesToRemove = append(indexesToRemove, vnodeIndex)
	}

	sort.Slice(indexesToRemove, func(i, j int) bool {
		// NOTE: this is a reverse sort!
		return indexesToRemove[j] < indexesToRemove[i]
	})

	if len(indexesToRemove) != int(h.replicationFactor) {
		panic(fmt.Sprintf("found wrong number of vnodes to remove: %d != %d", len(indexesToRemove), h.replicationFactor))
	}

	for i, indexToRemove := range indexesToRemove {
		// Swap this index for a later one
		h.virtualNodes[indexToRemove] = h.virtualNodes[len(h.virtualNodes)-1-i]
	}

	// Truncate and sort the nodelist
	h.virtualNodes = h.virtualNodes[:len(h.virtualNodes)-len(indexesToRemove)]
	sort.Sort(h.virtualNodes)

	// Remove the node from our map
	delete(h.nodes, nodeKeyString)

	return nil
}

// FindN finds the first N members in the hashring after the specified key.
//
// If there are not enough members in the hashring to satisfy the request,
// ErrNotEnoughMembers is returned.
func (h *Hashring) FindN(key []byte, num uint8) ([]Member, error) {
	h.RLock()
	defer h.RUnlock()

	if int(num) > len(h.nodes) {
		return nil, ErrNotEnoughMembers
	}

	keyHash := h.hasher(key)

	vnodeIndex := sort.Search(len(h.virtualNodes), func(i int) bool {
		return h.virtualNodes[i].hashvalue >= keyHash
	})

	alreadyFoundNodeKeys := map[string]struct{}{}
	foundNodes := make([]Member, 0, num)
	for i := 0; i < len(h.virtualNodes) && len(foundNodes) < int(num); i++ {
		boundedIndex := (i + vnodeIndex) % len(h.virtualNodes)
		candidate := h.virtualNodes[boundedIndex]
		if _, ok := alreadyFoundNodeKeys[candidate.members.nodeKey]; !ok {
			foundNodes = append(foundNodes, candidate.members.member)
			alreadyFoundNodeKeys[candidate.members.nodeKey] = struct{}{}
		}
	}

	return foundNodes, nil
}

// Members returns the current list of members of the Hashring.
func (h *Hashring) Members() []Member {
	h.RLock()
	defer h.RUnlock()

	membersCopy := make([]Member, 0, len(h.nodes))
	for _, nodeInfo := range h.nodes {
		membersCopy = append(membersCopy, nodeInfo.member)
	}
	return membersCopy
}
