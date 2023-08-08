/*
 *
 * Copyright 2022 gRPC authors.
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

package leastrequest

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/internal/grpcrand"
	"google.golang.org/grpc/serviceconfig"
)

// Name is the name of the least request balancer.
const Name = "least_request_experimental"

var logger = grpclog.Component("least request")

type bb struct{}

func (bb) ParseConfig(s json.RawMessage) (serviceconfig.LoadBalancingConfig, error) {
	lbConfig := &leastRequestConfig{
		ChoiceCount: 2,
	}
	if err := json.Unmarshal(s, lbConfig); err != nil {
		return nil, fmt.Errorf("least-request: unable to unmarshal LBConfig: %v", err)
	}
	// "If a LeastRequestLoadBalancingConfig with a choice_count > 10 is
	// received, the least_request_experimental policy will set choice_count =
	// 10."
	if lbConfig.ChoiceCount > 10 {
		lbConfig.ChoiceCount = 10
	}
	// I asked about this in chat but what happens if choiceCount < 2 (0 or 1)?
	// Doing this for now.
	if lbConfig.ChoiceCount < 2 {
		lbConfig.ChoiceCount = 2
	}
	return lbConfig, nil
}

func (bb) Name() string {
	return Name
}

func (bb) Build(cc balancer.ClientConn, bOpts balancer.BuildOptions) balancer.Balancer {
	b := &leastRequestBalancer{}
	baseBuilder := base.NewBalancerBuilder(Name, b, base.Config{HealthCheck: true})
	baseBalancer := baseBuilder.Build(cc, bOpts)
	b.Balancer = baseBalancer
	return b
}

type leastRequestBalancer struct {
	// Embeds balancer.Balancer because needs to intercept UpdateClientConnState
	// to learn about choiceCount.
	balancer.Balancer

	choiceCount uint32
}

type leastRequestConfig struct {
	serviceconfig.LoadBalancingConfig `json:"-"`

	// ChoiceCount is the number of random SubConns to sample to try and find
	// the one with the Least Request. If unset, defaults to 2. If set to < 2,
	// will become 2, and if set to > 10, will become 10.
	ChoiceCount uint32 `json:"choiceCount,omitempty"`
}

func (lrb *leastRequestBalancer) UpdateClientConnState(s balancer.ClientConnState) error {
	lrCfg, ok := s.BalancerConfig.(*leastRequestConfig)
	if !ok {
		logger.Errorf("least-request: received config with unexpected type %T: %v", s.BalancerConfig, s.BalancerConfig)
		return balancer.ErrBadResolverState
	}

	lrb.choiceCount = lrCfg.ChoiceCount
	return lrb.Balancer.UpdateClientConnState(s)
}

type scWithRPCCount struct {
	sc      balancer.SubConn
	numRPCs int32
}

func (lrb *leastRequestBalancer) Build(info base.PickerBuildInfo) balancer.Picker {
	logger.Infof("least-request: Build called with info: %v", info)
	if len(info.ReadySCs) == 0 {
		return base.NewErrPicker(balancer.ErrNoSubConnAvailable)
	}
	scs := make([]scWithRPCCount, 0, len(info.ReadySCs))
	for sc := range info.ReadySCs {
		scs = append(scs, scWithRPCCount{
			sc: sc,
		})
	}

	return &picker{
		choiceCount: lrb.choiceCount,
		subConns:    scs,
	}
}

type picker struct {
	// choiceCount is the number of random SubConns to find the one with
	// the least request.
	choiceCount uint32
	// Built out when receives list of ready RPCs.
	subConns []scWithRPCCount
}

func (p *picker) Pick(balancer.PickInfo) (balancer.PickResult, error) {
	var pickedSC *scWithRPCCount
	for i := 0; i < int(p.choiceCount); i++ {
		index := grpcrand.Uint32() % uint32(len(p.subConns))
		sc := p.subConns[index]
		if pickedSC == nil {
			pickedSC = &sc
			continue
		}
		if sc.numRPCs < pickedSC.numRPCs {
			pickedSC = &sc
		}
	}
	// "The counter for a subchannel should be atomically incremented by one
	// after it has been successfully picked by the picker." - A48
	atomic.AddInt32(&pickedSC.numRPCs, 1)

	// "the picker should add a callback for atomically decrementing the
	// subchannel counter once the RPC finishes (regardless of Status code)." -
	// A48.
	done := func(balancer.DoneInfo) {
		atomic.AddInt32(&pickedSC.numRPCs, -1)
	}

	return balancer.PickResult{
		SubConn: pickedSC.sc,
		Done:    done,
	}, nil
}
