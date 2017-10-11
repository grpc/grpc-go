/*
 *
 * Copyright 2017 gRPC authors.
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

package grpc

import (
	"encoding/json"
	"time"

	"google.golang.org/grpc/grpclog"
)

func parseTimeout(t *string) (*time.Duration, error) {
	if t == nil {
		return nil, nil
	}
	d, err := time.ParseDuration(*t)
	return &d, err
}

type jsonName struct {
	Service *string `json:"service,omitempty"`
	Method  *string `json:"method,omitempty"`
}

func (j jsonName) String() (string, bool) {
	if j.Service == nil {
		return "", false
	}
	res := "/" + *j.Service + "/"
	if j.Method != nil {
		res += *j.Method
	}
	return res, true
}

type jsonMC struct {
	Name                    *[]jsonName `json:"name,omitempty"`
	WaitForReady            *bool       `json:"waitForReady,omitempty"`
	Timeout                 *string     `json:"timeout,omitempty"`
	MaxRequestMessageBytes  *int        `json:"maxRequestMessageBytes,omitempty"`
	MaxResponseMessageBytes *int        `json:"maxResponseMessageBytes,omitempty"`
}

type jsonSC struct {
	LoadBalancingPolicy *string   `json:"loadBalancingPolicy,omitempty"`
	MethodConfig        *[]jsonMC `json:"methodConfig,omitempty"`
}

func parseServiceConfig(js string) (ServiceConfig, error) {
	var rsc jsonSC
	err := json.Unmarshal([]byte(js), &rsc)
	if err != nil {
		grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
		return ServiceConfig{}, err
	}
	sc := ServiceConfig{
		LB:      rsc.LoadBalancingPolicy,
		Methods: make(map[string]MethodConfig),
	}
	if rsc.MethodConfig == nil {
		return sc, nil
	}

	for _, m := range *rsc.MethodConfig {
		if m.Name == nil {
			continue
		}
		d, err := parseTimeout(m.Timeout)
		if err != nil {
			grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
			return ServiceConfig{}, err
		}

		mc := MethodConfig{
			WaitForReady: m.WaitForReady,
			Timeout:      d,
			MaxReqSize:   m.MaxRequestMessageBytes,
			MaxRespSize:  m.MaxResponseMessageBytes,
		}
		for _, n := range *m.Name {
			if path, valid := n.String(); valid {
				sc.Methods[path] = mc
			}
		}
	}

	return sc, nil
}
