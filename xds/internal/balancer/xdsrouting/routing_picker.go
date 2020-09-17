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

package xdsrouting

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// pickerGroup contains a list of route matchers and their corresponding
// pickers. For each pick, the first matched picker is used. If the picker isn't
// ready, the pick will be queued.
type pickerGroup struct {
	routes  []route
	pickers map[string]balancer.Picker
}

func newPickerGroup(routes []route, idToPickerState map[string]*subBalancerState) *pickerGroup {
	pickers := make(map[string]balancer.Picker)
	for id, st := range idToPickerState {
		pickers[id] = st.state.Picker
	}
	return &pickerGroup{
		routes:  routes,
		pickers: pickers,
	}
}

var errNoMatchedRouteFound = status.Errorf(codes.Unavailable, "no matched route was found")

func (pg *pickerGroup) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	for _, rt := range pg.routes {
		if rt.m.match(info) {
			// action from route is the ID for the sub-balancer to use.
			p, ok := pg.pickers[rt.action]
			if !ok {
				return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
			}
			return p.Pick(info)
		}
	}
	return balancer.PickResult{}, errNoMatchedRouteFound
}
