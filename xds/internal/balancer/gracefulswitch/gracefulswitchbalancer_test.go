package gracefulswitch

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/grpctest"
	"testing"
)

type s struct {
	grpctest.Tester
}


// cdsbalancer, balancergroup, rlsBalancer for examples

// What is main functionality?

// state: (what will I need to verify at certain situations?)

/*
type gracefulSwitchBalancer struct {
	bOpts          balancer.BuildOptions
	cc balancer.ClientConn

	outgoingMu sync.Mutex
	recentConfig *lbConfig
	balancerCurrent balancer.Balancer
	balancerPending balancer.Balancer

	incomingMu sync.Mutex
	scToSubBalancer map[balancer.SubConn]balancer.Balancer
	pendingState balancer.State

	closed *grpcsync.Event
}
*/

// Entrance functions into gracefulSwitchBalancer:

// UpdateClientConnState(state balancer.ClientConnState) error

// ResolverError(err error)

// UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState)

// Close()


// Causes balancerCurrent and balancerPending to cycle in and out of permutations

// Will communicate (ping) balancerCurrent/balancerPending and also ClientConn






// Basic test of first Update constructing something for current
func (s) TestFirstUpdate(t *testing.T) {
	tests := []struct {
		name string
		ccs balancer.ClientConnState
		wantErr error
	}{
		{
			name: "successful-first-update",
			ccs: balancer.ClientConnState{BalancerConfig: lbConfig{ChildBalancerType: balancerName1, Config: /*Any interesting logic here?*/}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			builder := balancer.Get("graceful_switch_load_balancer")
			gsb := builder.Build(/*Mock Client Conn here - should I use the one already in codebase?*/, balancer.BuildOptions{})
			err := gsb.UpdateClientConnState(test.ccs/*Client Conn State here that triggers a balancer down low to build or error, test.ccs*/)

			// Things that trigger error condition (I feel like none of these should happen in practice):


			// Balancer has already been closed
			// Wrong config itself
			// Wrong type inside the config




			// If successful, verify one of the two balancers down below (config will
			// specify right type) gets the right Update....ASSERT the balancer builds and calls UpdateClientConnState()...that should be the scope of this test

		})
	}


}


// Test that tests Update with 1 + Update with 1 = UpdateClientConnState twice

// UpdateState() causes it to forward to ClientConn




// Test that tests Update with 1 + Update with 2 = two balancers

// Pending being READY should cause it to switch current to pending (i.e. UpdateState())




// Test that tests Update with 1 + Update with 2 = two balancers

// Current leaving READY should cause it to switch current to pending (i.e. UpdateState())




// Test that tests UpdateState()






// Test that tests Close(), downstream effects of closing SubConns, and also guarding everything else



// Add/Remove Subconns



// Works normally in current system


// Mock balancer.ClientConn here





// Mock balancer.Balancer here (the current or pending balancer)

// register it, also have an unexported function that allows it to ping up to balancer.ClientConn (updateState() and newSubConn() eventually)
const balancerName1 = "mock_balancer_1" // <- put this as name of config

func init() {
	balancer.Register(bb1{})
}

type bb1 struct{}

func (bb1) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return &mockBalancer1{
		cc: &cc,
	}
}

func (bb1) Name() string {
	return balancerName1
}


type mockBalancer1 struct {
	// Hold onto Client Conn wrapper to communicate with it
	cc *balancer.ClientConn
}

func (mb1 *mockBalancer1) UpdateClientConnState(ccs balancer.ClientConnState) error {

}

func (mb1 *mockBalancer1) ResolverError(err error) {

}

func (mb1 *mockBalancer1) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {

}

func (mb1 *mockBalancer1) Close() {

}


// it's determined by config type, so need a second balancer here