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
//
// Experimental
//
// Notice: This package is EXPERIMENTAL and may be changed or removed
// in a later release.
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
	Key    string
	Values []string
}

type peer struct {
	Principals []string
}

type request struct {
	Paths   []string
	Headers []header
}

type rule struct {
	Name    string
	Source  peer
	Request request
}

// Represents the SDK authorization policy provided by user.
type authorizationPolicy struct {
	Name       string
	DenyRules  []rule `json:"deny_rules"`
	AllowRules []rule `json:"allow_rules"`
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
	switch {
	case value == "*":
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Prefix{},
		}
	case strings.HasSuffix(value, "*"):
		prefix := strings.TrimSuffix(value, "*")
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Prefix{Prefix: prefix},
		}
	case strings.HasPrefix(value, "*"):
		suffix := strings.TrimPrefix(value, "*")
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Suffix{Suffix: suffix},
		}
	default:
		return &v3matcherpb.StringMatcher{
			MatchPattern: &v3matcherpb.StringMatcher_Exact{Exact: value},
		}
	}
}

func getHeaderMatcher(key, value string) *v3routepb.HeaderMatcher {
	switch {
	case value == "*":
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{},
		}
	case strings.HasSuffix(value, "*"):
		prefix := strings.TrimSuffix(value, "*")
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_PrefixMatch{PrefixMatch: prefix},
		}
	case strings.HasPrefix(value, "*"):
		suffix := strings.TrimPrefix(value, "*")
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_SuffixMatch{SuffixMatch: suffix},
		}
	default:
		return &v3routepb.HeaderMatcher{
			Name:                 key,
			HeaderMatchSpecifier: &v3routepb.HeaderMatcher_ExactMatch{ExactMatch: value},
		}
	}
}

func parsePrincipalNames(principalNames []string) []*v3rbacpb.Principal {
	var ps []*v3rbacpb.Principal
	for _, principalName := range principalNames {
		newPrincipalName := &v3rbacpb.Principal{
			Identifier: &v3rbacpb.Principal_Authenticated_{
				Authenticated: &v3rbacpb.Principal_Authenticated{
					PrincipalName: getStringMatcher(principalName),
				},
			}}
		ps = append(ps, newPrincipalName)
	}
	return ps
}

func parsePeer(source peer) (*v3rbacpb.Principal, error) {
	if len(source.Principals) > 0 {
		return principalOr(parsePrincipalNames(source.Principals)), nil
	}
	return &v3rbacpb.Principal{
		Identifier: &v3rbacpb.Principal_Any{
			Any: true,
		},
	}, nil
}

func parsePaths(paths []string) []*v3rbacpb.Permission {
	var ps []*v3rbacpb.Permission
	for _, path := range paths {
		newPath := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_UrlPath{
				UrlPath: &v3matcherpb.PathMatcher{
					Rule: &v3matcherpb.PathMatcher_Path{Path: getStringMatcher(path)}}}}
		ps = append(ps, newPath)
	}
	return ps
}

func parseHeaderValues(key string, values []string) []*v3rbacpb.Permission {
	var vs []*v3rbacpb.Permission
	for _, value := range values {
		newHeader := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_Header{
				Header: getHeaderMatcher(key, value)}}
		vs = append(vs, newHeader)
	}
	return vs
}

var unsupportedHeaders = map[string]bool{
	"host":                true,
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

func unsupportedHeader(key string) bool {
	return key[0] == ':' || strings.HasPrefix(key, "grpc-") || unsupportedHeaders[key]
}

func parseHeaders(headers []header) ([]*v3rbacpb.Permission, error) {
	var hs []*v3rbacpb.Permission
	for i, header := range headers {
		if header.Key == "" {
			return nil, fmt.Errorf(`"headers" %d: "key" is not present`, i)
		}
		header.Key = strings.ToLower(header.Key)
		if unsupportedHeader(header.Key) {
			return nil, fmt.Errorf(`"headers" %d: unsupported "key" %s`, i, header.Key)
		}
		if len(header.Values) == 0 {
			return nil, fmt.Errorf(`"headers" %d: "values" is not present`, i)
		}
		values := parseHeaderValues(header.Key, header.Values)
		hs = append(hs, permissionOr(values))
	}
	return hs, nil
}

func parseRequest(request request) (*v3rbacpb.Permission, error) {
	var and []*v3rbacpb.Permission
	if len(request.Paths) > 0 {
		and = append(and, permissionOr(parsePaths(request.Paths)))
	}
	if len(request.Headers) > 0 {
		headers, err := parseHeaders(request.Headers)
		if err != nil {
			return nil, err
		}
		and = append(and, permissionAnd(headers))
	}
	if len(and) > 0 {
		return permissionAnd(and), nil
	}
	return &v3rbacpb.Permission{
		Rule: &v3rbacpb.Permission_Any{
			Any: true,
		},
	}, nil
}

func parseRules(rules []rule, prefixName string) (map[string]*v3rbacpb.Policy, error) {
	policies := make(map[string]*v3rbacpb.Policy)
	for i, rule := range rules {
		if rule.Name == "" {
			return policies, fmt.Errorf(`%d: "name" is not present`, i)
		}
		principal, err := parsePeer(rule.Source)
		if err != nil {
			return nil, fmt.Errorf("%d: %v", i, err)
		}
		permission, err := parseRequest(rule.Request)
		if err != nil {
			return nil, fmt.Errorf("%d: %v", i, err)
		}
		policyName := prefixName + "_" + rule.Name
		policies[policyName] = &v3rbacpb.Policy{
			Principals:  []*v3rbacpb.Principal{principal},
			Permissions: []*v3rbacpb.Permission{permission},
		}
	}
	return policies, nil
}

// translatePolicy translates SDK authorization policy in JSON format to two
// Envoy RBAC polices (deny and allow policy). If the policy cannot be parsed
// or is invalid, an error will be returned.
func translatePolicy(policyStr string) (*v3rbacpb.RBAC, *v3rbacpb.RBAC, error) {
	var policy authorizationPolicy
	if err := json.Unmarshal([]byte(policyStr), &policy); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal policy: %v", err)
	}
	if policy.Name == "" {
		return nil, nil, fmt.Errorf(`"name" is not present`)
	}
	if len(policy.AllowRules) == 0 {
		return nil, nil, fmt.Errorf(`"allow_rules" is not present`)
	}
	allowPolicies, err := parseRules(policy.AllowRules, policy.Name)
	if err != nil {
		return nil, nil, fmt.Errorf(`"allow_rules" %v`, err)
	}
	allowRBAC := &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_ALLOW, Policies: allowPolicies}
	var denyRBAC *v3rbacpb.RBAC
	if len(policy.DenyRules) > 0 {
		denyPolicies, err := parseRules(policy.DenyRules, policy.Name)
		if err != nil {
			return nil, nil, fmt.Errorf(`"deny_rules" %v`, err)
		}
		denyRBAC = &v3rbacpb.RBAC{
			Action:   v3rbacpb.RBAC_DENY,
			Policies: denyPolicies,
		}
	}
	return denyRBAC, allowRBAC, nil
}
