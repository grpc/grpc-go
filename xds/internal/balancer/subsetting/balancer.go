package subsetting

import (
	"errors"
	"fmt"
	"math/rand"

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

	// If resolver state contains no addresses, return an error so ClientConn
	// will trigger re-resolve. Also records this as an resolver error, so when
	// the overall state turns transient failure, the error message will have
	// the zero address information.
	if len(s.ResolverState.Addresses) == 0 {
		b.ResolverError(errors.New("produced zero addresses"))
		return balancer.ErrBadResolverState
	}

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
	subsetCount := uint64(len(s.Addresses)) / b.cfg.SubsetSize
	clientIndex := *b.cfg.ClientIndex

	addr := make([]resolver.Address, len(s.Addresses))
	copy(addr, s.Addresses)

	round := clientIndex / subsetCount
	r := rand.New(rand.NewSource(int64(round)))
	r.Shuffle(len(addr), func(i, j int) { addr[i], addr[j] = addr[j], addr[i] })

	subsetId := clientIndex % subsetCount

	start := int(subsetId * b.cfg.SubsetSize)
	end := start + int(b.cfg.SubsetSize)
	if end > len(addr) {
		addr = append(addr, addr[:(end-len(addr))]...)
	}
	return resolver.State{
		Addresses:     addr[start:end],
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
