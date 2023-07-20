package subsetting

import (
	"encoding/json"

	iserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

// LBConfig is the config for the outlier detection balancer.
type LBConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	ClientIndex *uint64 `json:"clientIndex,omitempty"`
	SubsetSize  uint64  `json:"subsetSize,omitempty"`

	ChildPolicy *iserviceconfig.BalancerConfig `json:"childPolicy,omitempty"`
}

// For UnmarshalJSON to work correctly and set defaults without infinite
// recursion.
type lbConfig LBConfig

func (lbc *LBConfig) UnmarshalJSON(j []byte) error {
	// Default top layer values.
	lbc.SubsetSize = 10
	// Unmarshal JSON on a type with zero values for methods, including
	// UnmarshalJSON. Overwrites defaults, leaves alone if not. typecast to
	// avoid infinite recursion by not recalling this function and causing stack
	// overflow.
	return json.Unmarshal(j, (*lbConfig)(lbc))
}
