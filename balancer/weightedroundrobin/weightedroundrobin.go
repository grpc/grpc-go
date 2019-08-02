package weightedroundrobin

// Name is the name of weighted_round_robin balancer.
const Name = "weighted_round_robin"

// Information that should be stored inside Address metadata in order to use wrr.
type AddrInfo struct{
	Weight uint32
}
