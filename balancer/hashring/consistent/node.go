package consistent

import "strings"

type nodeRecord struct {
	hashvalue    uint64
	nodeKey      string
	member       Member
	virtualNodes []virtualNode
}

type virtualNode struct {
	hashvalue uint64
	members   nodeRecord
}

func (vn virtualNode) less(rhs virtualNode) bool {
	if vn.hashvalue == rhs.hashvalue {
		if vn.members.hashvalue == rhs.members.hashvalue {
			return strings.Compare(vn.members.nodeKey, rhs.members.nodeKey) < 0
		}

		return vn.members.hashvalue < rhs.members.hashvalue
	}
	return vn.hashvalue < rhs.hashvalue
}

type virtualNodeList []virtualNode

func (vnl virtualNodeList) Len() int {
	return len(vnl)
}

func (vnl virtualNodeList) Less(i, j int) bool {
	return vnl[i].less(vnl[j])
}

func (vnl virtualNodeList) Swap(i, j int) {
	temp := vnl[i]
	vnl[i] = vnl[j]
	vnl[j] = temp
}
