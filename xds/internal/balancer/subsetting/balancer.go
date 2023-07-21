package subsetting

import (
	"fmt"
	"math/rand"
	"sort"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/internal/grpclog"
	"google.golang.org/grpc/resolver"
)

type subsettingBalancer struct {
	cc     balancer.ClientConn
	logger *grpclog.PrefixLogger
	cfg    *LBConfig

	child *gracefulswitch.Balancer
}

func (b *subsettingBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lbCfg, ok := s.BalancerConfig.(*LBConfig)
	if !ok {
		b.logger.Errorf("received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	// Reject whole config if child policy doesn't exist, don't persist it for
	// later.
	bb := balancer.Get(lbCfg.ChildPolicy.Name)
	if bb == nil {
		return fmt.Errorf("subsetting: child balancer %q not registered", lbCfg.ChildPolicy.Name)
	}

	if b.cfg == nil || b.cfg.ChildPolicy.Name != lbCfg.ChildPolicy.Name {
		err := b.child.SwitchTo(bb)
		if err != nil {
			return fmt.Errorf("subsetting: error switching to child of type %q: %v", lbCfg.ChildPolicy.Name, err)
		}
	}
	b.cfg = lbCfg

	err := b.child.UpdateClientConnState(balancer.ClientConnState{
		ResolverState:  b.prepareChildResolverState(s.ResolverState),
		BalancerConfig: b.cfg.ChildPolicy.Config,
	})

	return err
}

func (b *subsettingBalancer) prepareChildResolverState(s resolver.State) resolver.State {
	if len(s.Addresses) <= int(b.cfg.SubsetSize) {
		return s
	}
	backendCount := len(s.Addresses)
	addresses := make([]resolver.Address, backendCount)
	copy(addresses, s.Addresses)

	// sort address list by IP because the algorithm assumes that the initial
	// order of the addresses is the same for every client
	sort.Slice(addresses, func(i, j int) bool {
		return addresses[i].Addr < addresses[j].Addr
	})

	subsetCount := backendCount / int(b.cfg.SubsetSize)
	clientIndex := int(*b.cfg.ClientIndex)

	round := clientIndex / subsetCount

	excludedCount := backendCount % int(b.cfg.SubsetSize)
	excludedStart := (round * excludedCount) % backendCount
	excludedEnd := (excludedStart + excludedCount) % backendCount
	if excludedStart < excludedEnd {
		addresses = append(addresses[:excludedStart], addresses[excludedEnd:]...)
	} else {
		addresses = addresses[excludedEnd:excludedStart]
	}

	r := rand.New(rand.NewSource(int64(round)))
	r.Shuffle(len(addresses), func(i, j int) {
		addresses[i], addresses[j] = addresses[j], addresses[i]
	})

	subsetId := clientIndex % subsetCount

	start := int(subsetId * int(b.cfg.SubsetSize))
	end := start + int(b.cfg.SubsetSize)
	return resolver.State{
		Addresses:     addresses[start:end],
		ServiceConfig: s.ServiceConfig,
		Attributes:    s.Attributes,
	}
}

func (b *subsettingBalancer) ResolverError(err error) {
	b.child.ResolverError(err)
}

func (b *subsettingBalancer) UpdateSubConnState(sc balancer.SubConn, state balancer.SubConnState) {
	b.child.UpdateSubConnState(sc, state)
}

func (b *subsettingBalancer) Close() {
	b.child.Close()
}

func (b *subsettingBalancer) ExitIdle() {
	b.child.ExitIdle()
}
