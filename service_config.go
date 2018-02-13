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
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

const maxInt = int(^uint(0) >> 1)

// MethodConfig defines the configuration recommended by the service providers for a
// particular method.
//
// Deprecated: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
type MethodConfig struct {
	// WaitForReady indicates whether RPCs sent to this method should wait until
	// the connection is ready by default (!failfast). The value specified via the
	// gRPC client API will override the value set here.
	WaitForReady *bool
	// Timeout is the default timeout for RPCs sent to this method. The actual
	// deadline used will be the minimum of the value specified here and the value
	// set by the application via the gRPC client API.  If either one is not set,
	// then the other will be used.  If neither is set, then the RPC has no deadline.
	Timeout *time.Duration
	// MaxReqSize is the maximum allowed payload size for an individual request in a
	// stream (client->server) in bytes. The size which is measured is the serialized
	// payload after per-message compression (but before stream compression) in bytes.
	// The actual value used is the minimum of the value specified here and the value set
	// by the application via the gRPC client API. If either one is not set, then the other
	// will be used.  If neither is set, then the built-in default is used.
	MaxReqSize *int
	// MaxRespSize is the maximum allowed payload size for an individual response in a
	// stream (server->client) in bytes.
	MaxRespSize *int
	// RetryPolicy configures retry options for the method.
	RetryPolicy *RetryPolicy
}

// ServiceConfig is provided by the service provider and contains parameters for how
// clients that connect to the service should behave.
//
// Deprecated: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
type ServiceConfig struct {
	// LB is the load balancer the service providers recommends. The balancer specified
	// via grpc.WithBalancer will override this.
	LB *string

	// Methods contains a map for the methods in this service.  If there is an
	// exact match for a method (i.e. /service/method) in the map, use the
	// corresponding MethodConfig.  If there's no exact match, look for the
	// default config for the service (/service/) and use the corresponding
	// MethodConfig if it exists.  Otherwise, the method has no MethodConfig to
	// use.
	Methods map[string]MethodConfig

	stickinessMetadataKey *string

	// If a RetryThrottlingPolicy is provided, gRPC will automatically throttle
	// retry attempts and hedged RPCs when the client’s ratio of failures to
	// successes exceeds a threshold.
	//
	// For each server name, the gRPC client will maintain a token_count which is
	// initially set to maxTokens, and can take values between 0 and maxTokens.
	//
	// Every outgoing RPC (regardless of service or method invoked) will change
	// token_count as follows:
	//
	//   - Every failed RPC will decrement the token_count by 1.
	//   - Every successful RPC will increment the token_count by tokenRatio.
	//
	// If token_count is less than or equal to maxTokens / 2, then RPCs will not
	// be retried and hedged RPCs will not be sent.
	RetryThrottling *RetryThrottlingPolicy
}

// RetryPolicy defines the go-native version of the retry policy defined by the
// service config here:
// https://github.com/grpc/proposal/blob/master/A6-client-retries.md#integration-with-service-config
type RetryPolicy struct {
	// MaxAttempts is the maximum number of attempts, including the original RPC.
	//
	// This field is required and must be two or greater.
	MaxAttempts int

	// Exponential backoff parameters. The initial retry attempt will occur at
	// random(0, initialBackoffMS). In general, the nth attempt will occur at
	// random(0,
	//   min(initialBackoffMS*backoffMultiplier**(n-1), maxBackoffMS)).
	//
	// These fields are required and must be greater than zero.
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64

	// The set of status codes which may be retried.
	//
	// Status codes are specified as strings, e.g., "UNAVAILABLE".
	//
	// This field is required and must be non-empty.
	RetryableStatusCodes []codes.Code
	// Set used to store RetryableStatusCodes for easy lookup.
	retryableStatusCodes map[codes.Code]bool
}

type jsonRetryPolicy struct {
	MaxAttempts          int
	InitialBackoff       string
	MaxBackoff           string
	BackoffMultiplier    float64
	RetryableStatusCodes []codes.Code
}

// RetryThrottlingPolicy defines the go-native version of the retry throttling
// policy defined by the service config here:
// https://github.com/grpc/proposal/blob/master/A6-client-retries.md#integration-with-service-config
type RetryThrottlingPolicy struct {
	// The number of tokens starts at maxTokens. The token_count will always be
	// between 0 and maxTokens.
	//
	// This field is required and must be greater than zero.
	MaxTokens float64
	// The amount of tokens to add on each successful RPC. Typically this will
	// be some number between 0 and 1, e.g., 0.1.
	//
	// This field is required and must be greater than zero. Up to 3 decimal
	// places are supported.
	TokenRatio float64
}

func parseDuration(s *string) (*time.Duration, error) {
	if s == nil {
		return nil, nil
	}
	if !strings.HasSuffix(*s, "s") {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}
	ss := strings.SplitN((*s)[:len(*s)-1], ".", 3)
	if len(ss) > 2 {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}
	// hasDigits is set if either the whole or fractional part of the number is
	// present, since both are optional but one is required.
	hasDigits := false
	var d time.Duration
	if len(ss[0]) > 0 {
		i, err := strconv.ParseInt(ss[0], 10, 32)
		if err != nil {
			return nil, fmt.Errorf("malformed duration %q: %v", *s, err)
		}
		d = time.Duration(i) * time.Second
		hasDigits = true
	}
	if len(ss) == 2 && len(ss[1]) > 0 {
		if len(ss[1]) > 9 {
			return nil, fmt.Errorf("malformed duration %q", *s)
		}
		f, err := strconv.ParseInt(ss[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("malformed duration %q: %v", *s, err)
		}
		for i := 9; i > len(ss[1]); i-- {
			f *= 10
		}
		d += time.Duration(f)
		hasDigits = true
	}
	if !hasDigits {
		return nil, fmt.Errorf("malformed duration %q", *s)
	}

	return &d, nil
}

type jsonName struct {
	Service *string
	Method  *string
}

func (j jsonName) generatePath() (string, bool) {
	if j.Service == nil {
		return "", false
	}
	res := "/" + *j.Service + "/"
	if j.Method != nil {
		res += *j.Method
	}
	return res, true
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonMC struct {
	Name                    *[]jsonName
	WaitForReady            *bool
	Timeout                 *string
	MaxRequestMessageBytes  *int64
	MaxResponseMessageBytes *int64
	RetryPolicy             *jsonRetryPolicy
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonSC struct {
	LoadBalancingPolicy   *string
	StickinessMetadataKey *string
	MethodConfig          *[]jsonMC
	RetryThrottling       *RetryThrottlingPolicy
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

		stickinessMetadataKey: rsc.StickinessMetadataKey,
		RetryThrottling:       rsc.RetryThrottling,
	}
	if rsc.MethodConfig == nil {
		return sc, nil
	}

	for _, m := range *rsc.MethodConfig {
		if m.Name == nil {
			continue
		}
		d, err := parseDuration(m.Timeout)
		if err != nil {
			grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
			return ServiceConfig{}, err
		}

		mc := MethodConfig{
			WaitForReady: m.WaitForReady,
			Timeout:      d,
		}
		if rp := m.RetryPolicy; rp != nil {
			ib, err := parseDuration(&rp.InitialBackoff)
			if err != nil {
				grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
				return ServiceConfig{}, err
			}
			mb, err := parseDuration(&rp.MaxBackoff)
			if err != nil {
				grpclog.Warningf("grpc: parseServiceConfig error unmarshaling %s due to %v", js, err)
				return ServiceConfig{}, err
			}

			mc.RetryPolicy = &RetryPolicy{
				MaxAttempts:          rp.MaxAttempts,
				InitialBackoff:       *ib,
				MaxBackoff:           *mb,
				BackoffMultiplier:    rp.BackoffMultiplier,
				RetryableStatusCodes: rp.RetryableStatusCodes,
			}
		}
		if m.MaxRequestMessageBytes != nil {
			if *m.MaxRequestMessageBytes > int64(maxInt) {
				mc.MaxReqSize = newInt(maxInt)
			} else {
				mc.MaxReqSize = newInt(int(*m.MaxRequestMessageBytes))
			}
		}
		if m.MaxResponseMessageBytes != nil {
			if *m.MaxResponseMessageBytes > int64(maxInt) {
				mc.MaxRespSize = newInt(maxInt)
			} else {
				mc.MaxRespSize = newInt(int(*m.MaxResponseMessageBytes))
			}
		}
		for _, n := range *m.Name {
			if path, valid := n.generatePath(); valid {
				sc.Methods[path] = mc
			}
		}
	}
	return sc, nil
}

// postProcessSC sanitizes the sc provided.
func postProcessSC(sc *ServiceConfig) {
	if sc.RetryThrottling != nil {
		if sc.RetryThrottling.MaxTokens <= 0 ||
			sc.RetryThrottling.MaxTokens >= 1000 ||
			sc.RetryThrottling.TokenRatio <= 0 {
			// Illegal throttling config; disable throttling.
			sc.RetryThrottling = nil
		}
	}
	for _, mc := range sc.Methods {
		rp := mc.RetryPolicy
		if rp == nil ||
			rp.MaxAttempts <= 1 ||
			rp.InitialBackoff <= 0 ||
			rp.MaxBackoff <= 0 ||
			rp.BackoffMultiplier <= 0 ||
			len(rp.RetryableStatusCodes) == 0 {
			// Illegal retry config; disable for this method config.
			mc.RetryPolicy = nil
			continue
		}
		if rp.MaxAttempts > 5 {
			// TODO(retry): Make the max maxAttempts configurable.
			rp.MaxAttempts = 5
		}
		rp.retryableStatusCodes = make(map[codes.Code]bool)
		for _, code := range rp.RetryableStatusCodes {
			rp.retryableStatusCodes[code] = true
		}
	}
}

func min(a, b *int) *int {
	if *a < *b {
		return a
	}
	return b
}

func getMaxSize(mcMax, doptMax *int, defaultVal int) *int {
	if mcMax == nil && doptMax == nil {
		return &defaultVal
	}
	if mcMax != nil && doptMax != nil {
		return min(mcMax, doptMax)
	}
	if mcMax != nil {
		return mcMax
	}
	return doptMax
}

func newInt(b int) *int {
	return &b
}
