package subsetting

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the weiighted aperture balancer.
const Name = "subsetting_experimental"

func init() {
	balancer.Register(bb{})
}

type bb struct{}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &subsettingBalancer{
		cc: cc,
	}
	b.logger = prefixLogger(b)
	b.logger.Infof("Created")
	b.child = gracefulswitch.NewBalancer(cc, bOpts)
	return b
}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbCfg := &LBConfig{
		// Default top layer values.
		SubsetSize: 10,
	}

	if err := json.Unmarshal(s, lbCfg); err != nil { // Validates child config if present as well.
		return nil, fmt.Errorf("subsetting: unable to unmarshal LBconfig: %s, error: %v", string(s), err)
	}

	if lbCfg.ClientIndex == nil {
		return nil, fmt.Errorf("subsetting: clientIndex field is missing: %s", string(s))
	}

	return lbCfg, nil
}

func (bb) Name() string {
	return Name
}
