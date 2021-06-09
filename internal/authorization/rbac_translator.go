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

func getHeaderMatcher(key string, value string) *v3routepb.HeaderMatcher {
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

func parsePrincipalNames(principalNames interface{}) ([]*v3rbacpb.Principal, error) {
	principalNamesArray, ok := principalNames.([]interface{})
	if !ok {
		return nil, fmt.Errorf("\"principals\" is not an array")
	}
	var or []*v3rbacpb.Principal
	for i, principalName := range principalNamesArray {
		principalNameStr, ok := principalName.(string)
		if !ok {
			return nil, fmt.Errorf("\"principals\" %d: %v is not a string", i, principalName)
		}
		newPrincipalName := &v3rbacpb.Principal{
			Identifier: &v3rbacpb.Principal_Authenticated_{
				Authenticated: &v3rbacpb.Principal_Authenticated{
					PrincipalName: getStringMatcher(principalNameStr),
				},
			}}
		or = append(or, newPrincipalName)
	}
	return or, nil
}

func parsePeer(source interface{}) (*v3rbacpb.Principal, error) {
	s, ok := source.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("\"source\" is not an object")
	}
	var and []*v3rbacpb.Principal
	if p, found := s["principals"]; found {
		or, err := parsePrincipalNames(p)
		if err != nil {
			return nil, err
		}
		if len(or) > 0 {
			and = append(and, principalOr(or))
		}
	}
	if len(and) > 0 {
		return principalAnd(and), nil
	}
	return principalAny(), nil
}

func parsePaths(paths interface{}) ([]*v3rbacpb.Permission, error) {
	pathsArray, ok := paths.([]interface{})
	if !ok {
		return nil, fmt.Errorf("\"paths\" is not an array")
	}
	var or []*v3rbacpb.Permission
	for i, path := range pathsArray {
		pathStr, ok := path.(string)
		if !ok {
			return nil, fmt.Errorf("\"paths\" %d: %v is not a string", i, path)
		}
		newPath := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_UrlPath{
				UrlPath: &v3matcherpb.PathMatcher{Rule: &v3matcherpb.PathMatcher_Path{Path: getStringMatcher(pathStr)}}}}
		or = append(or, newPath)
	}
	return or, nil
}

func parseHeaderValues(key string, values interface{}) ([]*v3rbacpb.Permission, error) {
	valuesArray, ok := values.([]interface{})
	if !ok {
		return nil, fmt.Errorf("\"values\" is not an array")
	}
	var or []*v3rbacpb.Permission
	for i, value := range valuesArray {
		valueStr, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("\"values\" %d: %v is not a string", i, value)
		}
		newHeader := &v3rbacpb.Permission{
			Rule: &v3rbacpb.Permission_Header{
				Header: getHeaderMatcher(key, valueStr)}}
		or = append(or, newHeader)
	}
	return or, nil
}

func parseHeaders(headers interface{}) ([]*v3rbacpb.Permission, error) {
	headersArray, ok := headers.([]interface{})
	if !ok {
		return nil, fmt.Errorf("\"headers\" is not an array")
	}
	if len(headersArray) == 0 {
		return nil, fmt.Errorf("\"headers\" is empty")
	}
	var and []*v3rbacpb.Permission
	for i, header := range headersArray {
		h, _ := header.(map[string]interface{})
		key, found := h["key"]
		if !found {
			return nil, fmt.Errorf("\"headers\" %d: \"key\" is not present", i)
		}
		keyStr, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("\"headers\" %d: \"key\" %v is not a string", i, key)
		}
		// TODO(ashithasantosh): Return error for unsupported headers- "hop-by-hop",
		// pseudo headers, "Host" header and headers prefixed with "grpc-".
		values, found := h["values"]
		if !found {
			return nil, fmt.Errorf("\"headers\" %d: \"values\" is not present", i)
		}
		or, err := parseHeaderValues(keyStr, values)
		if err != nil {
			return nil, fmt.Errorf("\"headers\" %d: %v", i, err)
		}
		and = append(and, permissionOr(or))
	}
	return and, nil
}

func parseRequest(request interface{}) (*v3rbacpb.Permission, error) {
	var and []*v3rbacpb.Permission
	r, ok := request.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("\"request\" is not an object")
	}
	if p, found := r["paths"]; found {
		or, err := parsePaths(p)
		if err != nil {
			return nil, err
		}
		if len(or) > 0 {
			and = append(and, permissionOr(or))
		}
	}
	if h, found := r["headers"]; found {
		subAnd, err := parseHeaders(h)
		if err != nil {
			return nil, err
		}
		if len(subAnd) > 0 {
			and = append(and, permissionAnd(subAnd))
		}
	}
	if len(and) > 0 {
		return permissionAnd(and), nil
	}
	return permissionAny(), nil
}

func parseRulesArray(rules interface{}, prefixName string) (map[string]*v3rbacpb.Policy, error) {
	rulesArray, ok := rules.([]interface{})
	if !ok {
		return nil, fmt.Errorf("rules is not an array")
	}
	policies := make(map[string]*v3rbacpb.Policy)
	for i, r := range rulesArray {
		rule := r.(map[string]interface{})
		name, found := rule["name"]
		if !found {
			return policies, fmt.Errorf("%d: \"name\" is not present", i)
		}
		nameStr, ok := name.(string)
		if !ok {
			return policies, fmt.Errorf("%d: \"name\" %v is not a string", i, name)
		}
		nameStr = prefixName + "_" + nameStr
		var principals []*v3rbacpb.Principal
		if source, found := rule["source"]; found {
			principal, err := parsePeer(source)
			if err != nil {
				return nil, fmt.Errorf("%d: %v", i, err)
			}
			principals = append(principals, principal)
		} else {
			principals = append(principals, principalAny())
		}
		var permissions []*v3rbacpb.Permission
		if request, found := rule["request"]; found {
			permission, err := parseRequest(request)
			if err != nil {
				return nil, fmt.Errorf("%d: %v", i, err)
			}
			permissions = append(permissions, permission)
		} else {
			permissions = append(permissions, permissionAny())
		}
		policies[nameStr] = &v3rbacpb.Policy{
			Principals:  principals,
			Permissions: permissions,
		}
	}
	return policies, nil
}

// Translates a gRPC authorization policy in JSON string to two RBAC policies, a deny
// RBAC policy followed by an allow RBAC policy. If the policy cannot be parsed or is
// invalid, an error will be returned.
func translatePolicy(policy string) (*v3rbacpb.RBAC, *v3rbacpb.RBAC, error) {
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(policy), &jsonData); err != nil {
		return nil, nil, fmt.Errorf("failed to parse authorization policy")
	}
	prefixName, found := jsonData["name"]
	if !found {
		return nil, nil, fmt.Errorf("\"name\" is not present")
	}
	prefixNameStr, ok := prefixName.(string)
	if !ok {
		return nil, nil, fmt.Errorf("\"name\" %v is not a string", prefixName)
	}
	allowRules, found := jsonData["allow_rules"]
	if !found {
		return nil, nil, fmt.Errorf("\"allow_rules\" is not present")
	}
	allowPolicies, err := parseRulesArray(allowRules, prefixNameStr)
	if err != nil {
		return nil, nil, fmt.Errorf("\"allow_rules\" %v", err)
	}
	var denyPolicies map[string]*v3rbacpb.Policy
	if denyRules, found := jsonData["deny_rules"]; found {
		if denyPolicies, err = parseRulesArray(denyRules, prefixNameStr); err != nil {
			return nil, nil, fmt.Errorf("\"deny_rules\" %v", err)
		}
	}
	return &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_DENY, Policies: denyPolicies}, &v3rbacpb.RBAC{Action: v3rbacpb.RBAC_ALLOW, Policies: allowPolicies}, nil
}
