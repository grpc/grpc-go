/*
 *
 * Copyright 2020 gRPC authors.
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

package resolver

import (
	"encoding/json"
	"fmt"

	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

const (
	cdsName            = "cds_experimental"
	weightedTargetName = "weighted_target_experimental"
	xdsRoutingName     = "xds_routing_experimental"
)

type serviceConfig struct {
	LoadBalancingConfig balancerConfig `json:"loadBalancingConfig"`
}

type balancerConfig []map[string]interface{}

func newBalancerConfig(name string, config interface{}) balancerConfig {
	return []map[string]interface{}{{name: config}}
}

type weightedCDSBalancerConfig struct {
	Targets map[string]cdsWithWeight `json:"targets"`
}

type cdsWithWeight struct {
	Weight      uint32         `json:"weight"`
	ChildPolicy balancerConfig `json:"childPolicy"`
}

type cdsBalancerConfig struct {
	Cluster string `json:"cluster"`
}

type route struct {
	Path            *string                    `json:"path,omitempty"`
	Prefix          *string                    `json:"prefix,omitempty"`
	Regex           *string                    `json:"regex,omitempty"`
	CaseInsensitive bool                       `json:"caseInsensitive"`
	Headers         []*xdsclient.HeaderMatcher `json:"headers,omitempty"`
	Fraction        *wrapperspb.UInt32Value    `json:"matchFraction,omitempty"`
	Action          string                     `json:"action"`
}

type xdsActionConfig struct {
	ChildPolicy balancerConfig `json:"childPolicy"`
}

type xdsRoutingBalancerConfig struct {
	Action map[string]xdsActionConfig `json:"action"`
	Route  []*route                   `json:"route"`
}

func (r *xdsResolver) routesToJSON(routes []*xdsclient.Route) (string, error) {
	r.updateActions(newActionsFromRoutes(routes))

	// Generate routes.
	var rts []*route
	for _, rt := range routes {
		t := &route{
			Path:            rt.Path,
			Prefix:          rt.Prefix,
			Regex:           rt.Regex,
			Headers:         rt.Headers,
			CaseInsensitive: rt.CaseInsensitive,
		}

		if f := rt.Fraction; f != nil {
			t.Fraction = &wrapperspb.UInt32Value{Value: *f}
		}

		t.Action = r.getActionAssignedName(rt.Action)
		rts = append(rts, t)
	}

	// Generate actions.
	action := make(map[string]xdsActionConfig)
	for _, act := range r.actions {
		action[act.assignedName] = xdsActionConfig{
			ChildPolicy: weightedClusterToBalancerConfig(act.clustersWithWeights),
		}
	}

	sc := serviceConfig{
		LoadBalancingConfig: newBalancerConfig(
			xdsRoutingName, xdsRoutingBalancerConfig{
				Route:  rts,
				Action: action,
			},
		),
	}

	bs, err := json.Marshal(sc)
	if err != nil {
		return "", fmt.Errorf("failed to marshal json: %v", err)
	}
	return string(bs), nil
}

func weightedClusterToBalancerConfig(wc map[string]uint32) balancerConfig {
	// Even if WeightedCluster has only one entry, we still use weighted_target
	// as top level balancer, to avoid switching top policy between CDS and
	// weighted_target, causing TCP connection to be recreated.
	targets := make(map[string]cdsWithWeight)
	for name, weight := range wc {
		targets[name] = cdsWithWeight{
			Weight:      weight,
			ChildPolicy: newBalancerConfig(cdsName, cdsBalancerConfig{Cluster: name}),
		}
	}
	bc := newBalancerConfig(
		weightedTargetName, weightedCDSBalancerConfig{
			Targets: targets,
		},
	)
	return bc
}

func (r *xdsResolver) serviceUpdateToJSON(su serviceUpdate) (string, error) {
	return r.routesToJSON(su.Routes)
}
