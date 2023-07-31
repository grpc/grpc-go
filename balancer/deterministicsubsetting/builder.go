/*
 *
 * Copyright 2019 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package deterministicsubsetting

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the weiighted aperture balancer.
const Name = "deterministic_subsetting"

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

	// if someonw needs subsetSize == 1, he should use pick_first instead
	if lbCfg.SubsetSize < 2 {
		return nil, errors.New("subsetting: subsetSize must be >= 2")
	}

	return lbCfg, nil
}

func (bb) Name() string {
	return Name
}
