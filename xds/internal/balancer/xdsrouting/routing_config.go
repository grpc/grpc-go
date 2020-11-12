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
	"encoding/json"
	"fmt"

	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
	xdsclient "google.golang.org/grpc/xds/internal/client"
)

type actionConfig struct {
	// ChildPolicy is the child policy and it's config.
	ChildPolicy *internalserviceconfig.BalancerConfig
}

type int64Range struct {
	start, end int64
}

type headerMatcher struct {
	name string
	// matchSpecifiers
	invertMatch bool

	// At most one of the following has non-default value.
	exactMatch, regexMatch, prefixMatch, suffixMatch string
	rangeMatch                                       *int64Range
	presentMatch                                     bool
}

type routeConfig struct {
	// Path, Prefix and Regex can have at most one set. This is guaranteed by
	// config parsing.
	path, prefix, regex string
	// Indicates if prefix/path matching should be case insensitive. The default
	// is false (case sensitive).
	caseInsensitive bool

	headers  []headerMatcher
	fraction *uint32

	// Action is the name from the action list.
	action string
}

// lbConfig is the balancer config for xds routing policy.
type lbConfig struct {
	serviceconfig.LoadBalancingConfig
	routes  []routeConfig
	actions map[string]actionConfig
}

// The following structs with `JSON` in name are temporary structs to unmarshal
// json into. The fields will be read into lbConfig, to be used by the balancer.

// routeJSON is temporary struct for json unmarshal.
type routeJSON struct {
	// Path, Prefix and Regex can have at most one non-nil.
	Path, Prefix, Regex *string
	CaseInsensitive     bool
	// Zero or more header matchers.
	Headers       []*xdsclient.HeaderMatcher
	MatchFraction *wrapperspb.UInt32Value
	// Action is the name from the action list.
	Action string
}

// lbConfigJSON is temporary struct for json unmarshal.
type lbConfigJSON struct {
	Route  []routeJSON
	Action map[string]actionConfig
}

func (jc lbConfigJSON) toLBConfig() *lbConfig {
	var ret lbConfig
	for _, r := range jc.Route {
		var tempR routeConfig
		switch {
		case r.Path != nil:
			tempR.path = *r.Path
		case r.Prefix != nil:
			tempR.prefix = *r.Prefix
		case r.Regex != nil:
			tempR.regex = *r.Regex
		}
		tempR.caseInsensitive = r.CaseInsensitive
		for _, h := range r.Headers {
			var tempHeader headerMatcher
			switch {
			case h.ExactMatch != nil:
				tempHeader.exactMatch = *h.ExactMatch
			case h.RegexMatch != nil:
				tempHeader.regexMatch = *h.RegexMatch
			case h.PrefixMatch != nil:
				tempHeader.prefixMatch = *h.PrefixMatch
			case h.SuffixMatch != nil:
				tempHeader.suffixMatch = *h.SuffixMatch
			case h.RangeMatch != nil:
				tempHeader.rangeMatch = &int64Range{
					start: h.RangeMatch.Start,
					end:   h.RangeMatch.End,
				}
			case h.PresentMatch != nil:
				tempHeader.presentMatch = *h.PresentMatch
			}
			tempHeader.name = h.Name
			if h.InvertMatch != nil {
				tempHeader.invertMatch = *h.InvertMatch
			}
			tempR.headers = append(tempR.headers, tempHeader)
		}
		if r.MatchFraction != nil {
			tempR.fraction = &r.MatchFraction.Value
		}
		tempR.action = r.Action
		ret.routes = append(ret.routes, tempR)
	}
	ret.actions = jc.Action
	return &ret
}

func parseConfig(c json.RawMessage) (*lbConfig, error) {
	var tempConfig lbConfigJSON
	if err := json.Unmarshal(c, &tempConfig); err != nil {
		return nil, err
	}

	// For each route:
	// - at most one of path/prefix/regex.
	// - action is in action list.

	allRouteActions := make(map[string]bool)
	for _, r := range tempConfig.Route {
		var oneOfCount int
		if r.Path != nil {
			oneOfCount++
		}
		if r.Prefix != nil {
			oneOfCount++
		}
		if r.Regex != nil {
			oneOfCount++
		}
		if oneOfCount != 1 {
			return nil, fmt.Errorf("%d (not exactly one) of path/prefix/regex is set in route %+v", oneOfCount, r)
		}

		for _, h := range r.Headers {
			var oneOfCountH int
			if h.ExactMatch != nil {
				oneOfCountH++
			}
			if h.RegexMatch != nil {
				oneOfCountH++
			}
			if h.PrefixMatch != nil {
				oneOfCountH++
			}
			if h.SuffixMatch != nil {
				oneOfCountH++
			}
			if h.RangeMatch != nil {
				oneOfCountH++
			}
			if h.PresentMatch != nil {
				oneOfCountH++
			}
			if oneOfCountH != 1 {
				return nil, fmt.Errorf("%d (not exactly one) of header matcher specifier is set in route %+v", oneOfCountH, h)
			}
		}

		if _, ok := tempConfig.Action[r.Action]; !ok {
			return nil, fmt.Errorf("action %q from route %+v is not found in action list", r.Action, r)
		}
		allRouteActions[r.Action] = true
	}

	// Verify that actions are used by at least one route.
	for n := range tempConfig.Action {
		if _, ok := allRouteActions[n]; !ok {
			return nil, fmt.Errorf("action %q is not used by any route", n)
		}
	}

	return tempConfig.toLBConfig(), nil
}
