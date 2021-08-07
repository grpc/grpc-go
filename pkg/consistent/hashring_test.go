package consistent

import (
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/cespare/xxhash"
	"github.com/stretchr/testify/require"
)

type testNode struct {
	nodeKeyAndValue string
	addNodeError    error
}

func (tn testNode) Key() string {
	return tn.nodeKeyAndValue
}

func TestHashring(t *testing.T) {
	testCases := []struct {
		replicationFactor uint8
		nodes             []testNode
	}{
		{1, []testNode{}},
		{1, []testNode{{"key1", nil}}},
		{1, []testNode{{"key1", nil}, {"key2", nil}}},
		{20, []testNode{{"key1", nil}}},
		{20, []testNode{{"key1", nil}, {"key2", nil}}},
		{20, []testNode{{"key1", nil}, {"key1", ErrMemberAlreadyExists}}},
	}

	for _, tc := range testCases {
		t.Run(strconv.Itoa(int(tc.replicationFactor)), func(t *testing.T) {
			require := require.New(t)

			ring := NewHashring(xxhash.Sum64, tc.replicationFactor)

			require.NotNil(ring.hasher)
			require.Equal(tc.replicationFactor, ring.replicationFactor)
			require.Len(ring.virtualNodes, 0)
			require.Len(ring.nodes, 0)

			successfulNodes := map[string]struct{}{}
			for _, testNodeInfo := range tc.nodes {
				err := ring.Add(testNodeInfo)
				require.Equal(testNodeInfo.addNodeError, err)

				if err == nil {
					successfulNodes[testNodeInfo.nodeKeyAndValue] = struct{}{}
				}

				require.Len(ring.virtualNodes, len(successfulNodes)*int(tc.replicationFactor))
				require.Len(ring.nodes, len(successfulNodes))

				// Try the find function
				if len(successfulNodes) > 0 {
					found, err := ring.FindN([]byte("key1"), 1)
					require.NoError(err)
					require.Len(found, 1)
					require.Contains(successfulNodes, found[0].Key())
				}

				checkAllFound := map[string]struct{}{}
				for k, v := range successfulNodes {
					checkAllFound[k] = v
				}
				allFound, err := ring.FindN([]byte("key1"), uint8(len(successfulNodes)))
				require.NoError(err)
				require.Len(allFound, len(successfulNodes))

				for _, found := range allFound {
					require.Contains(checkAllFound, found.Key())
					delete(checkAllFound, found.Key())
				}

				require.Empty(checkAllFound)

				// Ask for more nodes than exist
				_, err = ring.FindN([]byte("1"), uint8(len(successfulNodes)+1))
				require.Equal(ErrNotEnoughMembers, err)
			}

			// Build a consistent hash that adds the nodes in reverse order
			reverseRing := NewHashring(xxhash.Sum64, tc.replicationFactor)
			for i := 0; i < len(tc.nodes); i++ {
				toAdd := tc.nodes[len(tc.nodes)-1-i]

				// We intentionally ignore the errors here to get to the same member state
				reverseRing.Add(toAdd)
			}

			// Check that the findValues match for a few keys in both the reverse built and normal
			if len(successfulNodes) > 0 {
				for i := 0; i < 100; i++ {
					key := []byte(strconv.Itoa(i))
					found, err := ring.FindN(key, 1)
					require.NoError(err)

					reverseFound, err := reverseRing.FindN(key, 1)
					require.NoError(err)

					require.Equal(found[0].Key(), reverseFound[0].Key())
				}
			}

			// Empty out the nodes
			for _, testNodeInfo := range tc.nodes {
				err := ring.Remove(testNodeInfo)
				if testNodeInfo.addNodeError == nil {
					require.NoError(err)
					delete(successfulNodes, testNodeInfo.nodeKeyAndValue)
				} else {
					require.Equal(ErrMemberNotFound, err)
				}

				require.Len(ring.virtualNodes, len(successfulNodes)*int(tc.replicationFactor))
				require.Len(ring.nodes, len(successfulNodes))
			}
		})
	}
}

const numTestKeys = 1_000_000

func TestBackendBalance(t *testing.T) {
	hasherFunc := xxhash.Sum64

	testCases := []int{1, 2, 3, 5, 10, 100}

	for _, numMembers := range testCases {
		t.Run(strconv.Itoa(numMembers), func(t *testing.T) {
			require := require.New(t)

			ring := NewHashring(hasherFunc, 100)

			memberKeyCount := map[member]int{}

			for memberNum := 0; memberNum < numMembers; memberNum++ {
				oneMember := member(memberNum)
				ring.Add(oneMember)
				memberKeyCount[oneMember] = 0
			}

			require.Len(ring.Members(), numMembers)

			for i := 0; i < numTestKeys; i++ {
				found, err := ring.FindN([]byte(strconv.Itoa(i)), 1)
				require.NoError(err)
				require.Len(found, 1)

				memberKeyCount[found[0].(member)]++
			}

			totalKeysDistributed := 0
			mean := float64(numTestKeys) / float64(numMembers)
			stddevSum := 0.0
			for _, memberKeyCount := range memberKeyCount {
				totalKeysDistributed += memberKeyCount
				stddevSum += math.Pow(float64(memberKeyCount)-mean, 2)
			}
			require.Equal(numTestKeys, totalKeysDistributed)

			stddev := math.Sqrt(stddevSum / float64(numMembers))

			// We want the stddev to be less than 10% of the mean with 100 virtual nodes
			require.Less(stddev, mean*.1)
		})
	}
}

type member int

func (m member) Key() string {
	return fmt.Sprintf("member-%d", m)
}
