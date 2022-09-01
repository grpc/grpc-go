package roundrobin

import (
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/balancer"
	"math"
	"testing"
)

type testSubConn struct {
	balancer.SubConn
	index int
}

func makeTestSuConnArray(len int) []balancer.SubConn {
	var conns []balancer.SubConn
	for i := 0; i < len; i++ {
		conns = append(conns, &testSubConn{index: i})
	}
	return conns
}

func Test_Pick(t *testing.T) {
	conns := makeTestSuConnArray(10)
	p := &rrPicker{
		subConns: conns,
		next:     0,
	}

	var pickInfo balancer.PickInfo
	result, _ := p.Pick(pickInfo)
	assert.Equal(t, result.SubConn.(*testSubConn).index, 1)

	p.next = math.MaxUint32
	result, _ = p.Pick(pickInfo)
	assert.Equal(t, result.SubConn.(*testSubConn).index, 0)
}
