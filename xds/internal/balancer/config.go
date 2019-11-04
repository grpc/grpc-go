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
 */

package balancer

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/serviceconfig"
)

// XDSConfig represents the loadBalancingConfig section of the service config
// for xDS balancers.
type XDSConfig struct {
	serviceconfig.LoadBalancingConfig
	// BalancerName represents the load balancer to use.
	BalancerName string
	// ChildPolicy represents the load balancing config for the child
	// policy.
	ChildPolicy *loadBalancingConfig
	// FallBackPolicy represents the load balancing config for the
	// fallback.
	FallBackPolicy *loadBalancingConfig
	// Name to use in EDS query.  If not present, defaults to the server
	// name from the target URI.
	EdsServiceName string
	// LRS server to send load reports to.  If not present, load reporting
	// will be disabled.  If set to the empty string, load reporting will
	// be sent to the same server that we obtained CDS data from.
	LrsLoadReportingServerName string
}

// UnmarshalJSON parses the JSON-encoded byte slice in data and stores it in l.
// When unmarshalling, we iterate through the childPolicy/fallbackPolicy lists
// and select the first LB policy which has been registered.
func (l *XDSConfig) UnmarshalJSON(data []byte) error {
	var val map[string]json.RawMessage
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	for k, v := range val {
		switch k {
		case "balancerName":
			if err := json.Unmarshal(v, &l.BalancerName); err != nil {
				return err
			}
		case "childPolicy":
			var lbcfgs []*loadBalancingConfig
			if err := json.Unmarshal(v, &lbcfgs); err != nil {
				return err
			}
			for _, lbcfg := range lbcfgs {
				if balancer.Get(lbcfg.Name) != nil {
					l.ChildPolicy = lbcfg
					break
				}
			}
		case "fallbackPolicy":
			var lbcfgs []*loadBalancingConfig
			if err := json.Unmarshal(v, &lbcfgs); err != nil {
				return err
			}
			for _, lbcfg := range lbcfgs {
				if balancer.Get(lbcfg.Name) != nil {
					l.FallBackPolicy = lbcfg
					break
				}
			}
		case "edsServiceName":
			if err := json.Unmarshal(v, &l.EdsServiceName); err != nil {
				return err
			}
		case "lrsLoadReportingServerName":
			if err := json.Unmarshal(v, &l.LrsLoadReportingServerName); err != nil {
				return err
			}
		}
	}
	return nil
}

// MarshalJSON returns a JSON encoding of l.
func (l *XDSConfig) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("XDSConfig.MarshalJSON() is unimplemented")
}

// loadBalancingConfig represents a single load balancing config,
// stored in JSON format.
type loadBalancingConfig struct {
	Name   string
	Config json.RawMessage
}

// MarshalJSON returns a JSON encoding of l.
func (l *loadBalancingConfig) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	m[l.Name] = l.Config
	return json.Marshal(m)
}

// UnmarshalJSON parses the JSON-encoded byte slice in data and stores it in l.
func (l *loadBalancingConfig) UnmarshalJSON(data []byte) error {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	for name, config := range cfg {
		l.Name = name
		l.Config = config
	}
	return nil
}
