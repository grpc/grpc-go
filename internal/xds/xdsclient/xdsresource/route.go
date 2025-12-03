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
 */

package xdsresource

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/internal/wrr"
	"google.golang.org/grpc/internal/xds/httpfilter"
)

// MatchedRoute holds configuration for a single route selected by the xDS
// resolver's ConfigSelector.
type MatchedRoute struct {
	M                 *CompositeMatcher // converted from route matchers
	ActionType        RouteActionType   // holds route action type
	Clusters          wrr.WRR           // holds *routeCluster entries
	MaxStreamDuration time.Duration
	// map from filter name to its config
	HTTPFilterConfigOverride map[string]httpfilter.FilterConfig
	RetryConfig              *RetryConfig
	HashPolicies             []*HashPolicy
	AutoHostRewrite          bool
}

func (r MatchedRoute) String() string {
	return fmt.Sprintf("%s -> { clusters: %v, maxStreamDuration: %v }", r.M.String(), r.Clusters, r.MaxStreamDuration)
}

// XdsRouteAttributeKey is the context key used to store the MatchedRoute
// in the RPC context.
type XdsRouteAttributeKey struct{}

// GetMatchedRoute retrieves the MatchedRoute from the provided context.
func GetMatchedRoute(ctx context.Context) MatchedRoute {
	route, _ := ctx.Value(XdsRouteAttributeKey{}).(MatchedRoute)
	return route
}

// GetMatchedRouteForTesting returns the matched route in the context; to be
// used for testing only.
func GetMatchedRouteForTesting(ctx context.Context) MatchedRoute {
	return GetMatchedRoute(ctx)
}

// SetMatchedRoute adds the mathced route to the context for the
// xds_cluster_impl LB policy to pick.
func SetMatchedRoute(ctx context.Context, route MatchedRoute) context.Context {
	return context.WithValue(ctx, XdsRouteAttributeKey{}, route)
}
