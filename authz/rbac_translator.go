/*
 * Copyright 2021 gRPC authors.
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

// Package authz exposes methods to manage authorization within gRPC.
package authz

import (
	"encoding/json"
	"fmt"
	"strings"

	v3rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	v3routepb "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	v3matcherpb "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
)

type header struct {
	Key    *string   `json:"key"`
	Values *[]string `json:"values"`
}

type peer struct {
	Principals *[]string `json:"principals"`
}

type request struct {
	Paths   *[]string `json:"paths"`
	Headers *[]header `json:"headers"`
}

type rule struct {
	Name    *string  `json:"name"`
	Source  *peer    `json:"source"`
	Request *request `json:"request"`
}

// Represents the SDK authorization policy provided by user.
type authorizationPolicy struct {
	Name       *string `json:"name"`
	DenyRules  *[]rule `json:"deny_rules"`
	AllowRules *[]rule `json:"allow_rules"`
}

func principalAny() *v3rbacpb.Principal {
	return &v3rbacpb.Principal{
		Identifier: &v3rbacpb.Principal_Any{
			Any: true,
		},
	}
}

func principalOr(principals []*v3rbacpb.Principal) *v3rbacpb.Principal {
	return &v3rbacpb.Principal{
		Identifier: &v3rbacpb.Principal_OrIds{
			OrIds: &v3rbacpb.Principal_Set{
				Ids: principals,
			},
		},
	}
}

func principalAnd(principals []*v3rbacpb.Principal) *v3rbacpb.Principal {
	return &v3rbacpb.Principal{
		Identifier: &v3rbacpb.Principal_AndIds{
			AndIds: &v3rbacpb.Principal_Set{
				Ids: principals,
			},
		},
	}
}

func permissionAny() *v3rbacpb.Permission {
	return &v3rbacpb.Permission{
		Rule: &v3rbacpb.Permission_Any{
			Any: true,
		},
	}
}

func permissionOr(permission []*v3rbacpb.Permission) *v3rbacpb.Permission {
	return &v3rbacpb.Permission{
		Rule: &v3rbacpb.Permission_OrRules{
			OrRules: &v3rbacpb.Permission_Set{
				Rules: permission,
			},
		},
	}
}

func permissionAnd(permission []*v3rbacpb.Permission) *v3rbacpb.Permission {
	return &v3rbacpb.Permission{
		Rule: &v3rbacpb.Permission_AndRules{
			AndRules: &v3rbacpb.Permission_Set{
				Rules: permission,
			},
		},
	}
}

func getStringMatcher(value string) *v3matcherpb.StringMatcher {
	if value == "*" {
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Prefix{
				Prefix: "",
			},
		}
	} else if strings.HasSuffix(value, "*") {
		prefix := strings.TrimSuffix(value, "*")
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Prefix{
				Prefix: prefix,
			},
		}
	} else if strings.HasPrefix(value, "*") {
		suffix := strings.TrimPrefix(value, "*")
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Suffix{
				Suffix: suffix,
			},
		}
	}
	return &v3matcherpb.StringMatcher{
		MatchPattern: &v3matcherpb.StringMatcher_Exact{
			Exact: value,
		},
	}
}

func getHeaderMatcher(key, value string) *v3routepb.HeaderMatcher {
	if value == "*" {
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: ""},
		}
	} else if strings.HasSuffix(value, "*") {
		prefix := strings.TrimSuffix(value, "*")
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: prefix},
		}
	} else if strings.HasPrefix(value, "*") {
		suffix := strings.TrimPrefix(value, "*")
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{SuffixMatch: suffix},
		}
	}
	return &v3routepb.HeaderMatcher{
		Name:                 key,
		HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: value},
	}
}

func parsePrincipalNames(principalNames *[]string) []*v3rbacpb.Principal {
	var or []*v3rbacpb.Principal
	for _, principalName := range *principalNames {
		newPrincipalName := &v3rbacpb.Principal{
			Identifier: &v3rbacpb.Principal_Authenticated_{
				Authenticated: &v3rbacpb.Principal_Authenticated{
					PrincipalName: getStringMatcher(principalName),
				},
			}}
		or = append(or, newPrincipalName)
	}
	return or
}

func parsePeer(source *peer) *v3rbacpb.Principal {
	var and []*v3rbacpb.Principal
	if source != nil && source.Principals != nil {
		or := parsePrincipalNames(source.Principals)
		if len(or) > 0 {
			and = append(and, principalOr(or))
		}
	}
	if len(and) > 0 {
		return principalAnd(and)
	}
	return principalAny()
}

func parsePaths(paths *[]string) []*v3rbacpb.Permission {
	var or []*v3rbacpb.Permission
	for _, path := range *paths {
		newPath := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_UrlPath{
				UrlPath: &v3matcherpb.PathMatcher{
					Rule: &v3matcherpb.PathMatcher_Path{Path: getStringMatcher(path)}}}}
		or = append(or, newPath)
	}
	return or
}

func parseHeaderValues(key *string, values *[]string) []*v3rbacpb.Permission {
	var or []*v3rbacpb.Permission
	for _, value := range *values {
		newHeader := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_Header{
				Header: getHeaderMatcher(*key, value)}}
		or = append(or, newHeader)
	}
	return or
}

func parseHeaders(headers *[]header) ([]*v3rbacpb.Permission, error) {
	var and []*v3rbacpb.Permission
	for i, header := range *headers {
		if header.Key == nil {
			return nil, fmt.Errorf("\"headers\" %d: \"key\" is not present", i)
		}
		// TODO(ashithasantosh): Return error for unsupported headers- "hop-by-hop",
		// pseudo headers, "Host" header and headers prefixed with "grpc-".
		if header.Values == nil {
			return nil, fmt.Errorf("\"headers\" %d: \"values\" is not present", i)
		}
		and = append(and, permissionOr(parseHeaderValues(header.Key, header.Values)))
	}
	return and, nil
}

func parseRequest(request *request) (*v3rbacpb.Permission, error) {
	var and []*v3rbacpb.Permission
	if request != nil {
		if request.Paths != nil {
			or := parsePaths(request.Paths)
			if len(or) > 0 {
				and = append(and, permissionOr(or))
			}
		}
		if request.Headers != nil {
			subAnd, err := parseHeaders(request.Headers)
			if err != nil {
				return nil, err
			}
			if len(subAnd) > 0 {
				and = append(and, permissionAnd(subAnd))
			}
		}
	}
	if len(and) > 0 {
		return permissionAnd(and), nil
	}
	return permissionAny(), nil
}

func parseRulesArray(rules *[]rule, prefixName *string) (map[string]*v3rbacpb.Policy, error) {
	policies := make(map[string]*v3rbacpb.Policy)
	for i, rule := range *rules {
		if rule.Name == nil {
			return policies, fmt.Errorf("%d: \"name\" is not present", i)
		}
		var principals []*v3rbacpb.Principal
		principals = append(principals, parsePeer(rule.Source))
		var permissions []*v3rbacpb.Permission
		permission, err := parseRequest(rule.Request)
		if err != nil {
			return nil, fmt.Errorf("%d: %v", i, err)
		}
		permissions = append(permissions, permission)
		policyName := *prefixName + "_" + *rule.Name
		policies[policyName] = &v3rbacpb.Policy{
			Principals:  principals,
			Permissions: permissions,
		}
	}
	return policies, nil
}

// translatePolicy translates SDK authorization policy in JSON format to two Envoy RBAC polices (deny
// and allow policy). If the policy cannot be parsed or is invalid, an error will be returned.
func translatePolicy(policy string) (*v3rbacpb.RBAC, *v3rbacpb.RBAC, error) {
	var data authorizationPolicy
	if err := json.Unmarshal([]byte(policy), &data); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal policy: %v", err)
	}
	if data.Name == nil {
		return nil, nil, fmt.Errorf("\"name\" is not present")
	}
	if data.AllowRules == nil {
		return nil, nil, fmt.Errorf("\"allow_rules\" is not present")
	}
	allowPolicies, err := parseRulesArray(data.AllowRules, data.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("\"allow_rules\" %v", err)
	}
	allowRbac := &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_ALLOW, Policies: allowPolicies}
	var denyRbac *v3rbacpb.RBAC
	if data.DenyRules != nil {
		denyPolicies, err := parseRulesArray(data.DenyRules, data.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("\"deny_rules\" %v", err)
		}
		denyRbac = &v3rbacpb.RBAC{
			Action:   v3rbacpb.RBAC_DENY,
			Policies: denyPolicies}
	}
	return denyRbac, allowRbac, nil
}
