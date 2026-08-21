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
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/pickfirst"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/balancer/gracefulswitch"
	internalserviceconfig "google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/serviceconfig"
)

const maxInt = int(^uint(0) >> 1)

// jsonUint32 accepts numeric, string, and null forms allowed by ProtoJSON.
type jsonUint32 uint32

func (j *jsonUint32) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		*j = 0
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}

	value, err := parseJSONUint32(number.String())
	if err != nil {
		return err
	}
	*j = jsonUint32(value)
	return nil
}

func parseJSONUint32(number string) (uint32, error) {
	raw := number
	if number == "" {
		return 0, fmt.Errorf("invalid uint32 value %q", raw)
	}

	negative := number[0] == '-'
	if negative {
		number = number[1:]
	}

	exponentText := ""
	if i := strings.IndexAny(number, "eE"); i >= 0 {
		exponentText = number[i+1:]
		number = number[:i]
	}

	integerPart := number
	fractionalPart := ""
	if i := strings.IndexByte(number, '.'); i >= 0 {
		integerPart = number[:i]
		fractionalPart = strings.TrimRight(number[i+1:], "0")
	}

	// Match ProtoJSON handling of zero, including negative zero and zero with
	// an exponent that is too large to fit in an integer.
	if integerPart == "0" {
		integerPart = ""
	}
	if integerPart == "" && fractionalPart == "" {
		return 0, nil
	}

	exponent := 0
	if exponentText != "" {
		parsed, err := strconv.ParseInt(exponentText, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid uint32 value %q", raw)
		}
		exponent = int(parsed)
	}

	var digits string
	if exponent >= 0 {
		// Match ProtoJSON's bounded normalization before applying the final
		// uint32 range check.
		const maxDigits = 20
		if len(fractionalPart) > exponent ||
			len(integerPart) > maxDigits ||
			exponent > maxDigits-len(integerPart) {
			return 0, fmt.Errorf("invalid uint32 value %q", raw)
		}

		digits = integerPart + fractionalPart +
			strings.Repeat("0", exponent-len(fractionalPart))
	} else {
		if len(fractionalPart) > 0 {
			return 0, fmt.Errorf("invalid uint32 value %q", raw)
		}

		decimalPoint := len(integerPart) + exponent
		if decimalPoint < 0 {
			return 0, fmt.Errorf("invalid uint32 value %q", raw)
		}

		for _, digit := range integerPart[decimalPoint:] {
			if digit != '0' {
				return 0, fmt.Errorf("invalid uint32 value %q", raw)
			}
		}
		digits = integerPart[:decimalPoint]
	}

	if digits == "" {
		return 0, nil
	}
	if negative {
		return 0, fmt.Errorf("invalid uint32 value %q", raw)
	}

	value, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid uint32 value %q", raw)
	}
	return uint32(value), nil
}

// MethodConfig defines the configuration recommended by the service providers for a
// particular method.
//
// Deprecated: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
type MethodConfig = internalserviceconfig.MethodConfig

// ServiceConfig is provided by the service provider and contains parameters for how
// clients that connect to the service should behave.
//
// Deprecated: Users should not use this struct. Service config should be received
// through name resolver, as specified here
// https://github.com/grpc/grpc/blob/master/doc/service_config.md
type ServiceConfig struct {
	serviceconfig.Config

	// lbConfig is the service config's load balancing configuration.  If
	// lbConfig and LB are both present, lbConfig will be used.
	lbConfig serviceconfig.LoadBalancingConfig

	// Methods contains a map for the methods in this service.  If there is an
	// exact match for a method (i.e. /service/method) in the map, use the
	// corresponding MethodConfig.  If there's no exact match, look for the
	// default config for the service (/service/) and use the corresponding
	// MethodConfig if it exists.  Otherwise, the method has no MethodConfig to
	// use.
	Methods map[string]MethodConfig

	// If a retryThrottlingPolicy is provided, gRPC will automatically throttle
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
	retryThrottling *retryThrottlingPolicy
	// healthCheckConfig must be set as one of the requirement to enable LB channel
	// health check.
	healthCheckConfig *healthCheckConfig
	// rawJSONString stores service config json string that get parsed into
	// this service config struct.
	rawJSONString string
}

// healthCheckConfig defines the go-native version of the LB channel health check config.
type healthCheckConfig struct {
	// serviceName is the service name to use in the health-checking request.
	ServiceName string
}

type jsonRetryPolicy struct {
	MaxAttempts          jsonUint32
	InitialBackoff       internalserviceconfig.Duration
	MaxBackoff           internalserviceconfig.Duration
	BackoffMultiplier    float64
	RetryableStatusCodes []codes.Code
}

// retryThrottlingPolicy defines the go-native version of the retry throttling
// policy defined by the service config here:
// https://github.com/grpc/proposal/blob/master/A6-client-retries.md#integration-with-service-config
type retryThrottlingPolicy struct {
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

type jsonName struct {
	Service string
	Method  string
}

var (
	errDuplicatedName             = errors.New("duplicated name")
	errEmptyServiceNonEmptyMethod = errors.New("cannot combine empty 'service' and non-empty 'method'")
)

func (j jsonName) generatePath() (string, error) {
	if j.Service == "" {
		if j.Method != "" {
			return "", errEmptyServiceNonEmptyMethod
		}
		return "", nil
	}
	res := "/" + j.Service + "/"
	if j.Method != "" {
		res += j.Method
	}
	return res, nil
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonMC struct {
	Name                    *[]jsonName
	WaitForReady            *bool
	Timeout                 *internalserviceconfig.Duration
	MaxRequestMessageBytes  *jsonUint32
	MaxResponseMessageBytes *jsonUint32
	RetryPolicy             *jsonRetryPolicy
}

// TODO(lyuxuan): delete this struct after cleaning up old service config implementation.
type jsonSC struct {
	LoadBalancingPolicy *string
	LoadBalancingConfig *json.RawMessage
	MethodConfig        *[]jsonMC
	RetryThrottling     *retryThrottlingPolicy
	HealthCheckConfig   *healthCheckConfig
}

func init() {
	internal.ParseServiceConfig = func(js string) *serviceconfig.ParseResult {
		return parseServiceConfig(js, defaultMaxCallAttempts)
	}
}

func parseServiceConfig(js string, maxAttempts int) *serviceconfig.ParseResult {
	if len(js) == 0 {
		return &serviceconfig.ParseResult{Err: fmt.Errorf("no JSON service config provided")}
	}
	var rsc jsonSC
	err := json.Unmarshal([]byte(js), &rsc)
	if err != nil {
		logger.Warningf("grpc: unmarshalling service config %s: %v", js, err)
		return &serviceconfig.ParseResult{Err: err}
	}
	sc := ServiceConfig{
		Methods:           make(map[string]MethodConfig),
		retryThrottling:   rsc.RetryThrottling,
		healthCheckConfig: rsc.HealthCheckConfig,
		rawJSONString:     js,
	}
	c := rsc.LoadBalancingConfig
	if c == nil {
		name := pickfirst.Name
		if rsc.LoadBalancingPolicy != nil {
			name = *rsc.LoadBalancingPolicy
		}
		if balancer.Get(name) == nil {
			name = pickfirst.Name
		}
		cfg := []map[string]any{{name: struct{}{}}}
		strCfg, err := json.Marshal(cfg)
		if err != nil {
			return &serviceconfig.ParseResult{Err: fmt.Errorf("unexpected error marshaling simple LB config: %w", err)}
		}
		r := json.RawMessage(strCfg)
		c = &r
	}
	cfg, err := gracefulswitch.ParseConfig(*c)
	if err != nil {
		return &serviceconfig.ParseResult{Err: err}
	}
	sc.lbConfig = cfg

	if rsc.MethodConfig == nil {
		return &serviceconfig.ParseResult{Config: &sc}
	}

	paths := map[string]struct{}{}
	for _, m := range *rsc.MethodConfig {
		if m.Name == nil {
			continue
		}

		mc := MethodConfig{
			WaitForReady: m.WaitForReady,
			Timeout:      (*time.Duration)(m.Timeout),
		}
		if mc.RetryPolicy, err = convertRetryPolicy(m.RetryPolicy, maxAttempts); err != nil {
			logger.Warningf("grpc: unmarshalling service config %s: %v", js, err)
			return &serviceconfig.ParseResult{Err: err}
		}
		if m.MaxRequestMessageBytes != nil {
			if uint64(*m.MaxRequestMessageBytes) > uint64(maxInt) {
				mc.MaxReqSize = newInt(maxInt)
			} else {
				mc.MaxReqSize = newInt(int(*m.MaxRequestMessageBytes))
			}
		}
		if m.MaxResponseMessageBytes != nil {
			if uint64(*m.MaxResponseMessageBytes) > uint64(maxInt) {
				mc.MaxRespSize = newInt(maxInt)
			} else {
				mc.MaxRespSize = newInt(int(*m.MaxResponseMessageBytes))
			}
		}
		for i, n := range *m.Name {
			path, err := n.generatePath()
			if err != nil {
				logger.Warningf("grpc: error unmarshalling service config %s due to methodConfig[%d]: %v", js, i, err)
				return &serviceconfig.ParseResult{Err: err}
			}

			if _, ok := paths[path]; ok {
				err = errDuplicatedName
				logger.Warningf("grpc: error unmarshalling service config %s due to methodConfig[%d]: %v", js, i, err)
				return &serviceconfig.ParseResult{Err: err}
			}
			paths[path] = struct{}{}
			sc.Methods[path] = mc
		}
	}

	if sc.retryThrottling != nil {
		if mt := sc.retryThrottling.MaxTokens; mt <= 0 || mt > 1000 {
			return &serviceconfig.ParseResult{Err: fmt.Errorf("invalid retry throttling config: maxTokens (%v) out of range (0, 1000]", mt)}
		}
		if tr := sc.retryThrottling.TokenRatio; tr <= 0 {
			return &serviceconfig.ParseResult{Err: fmt.Errorf("invalid retry throttling config: tokenRatio (%v) may not be negative", tr)}
		}
	}
	return &serviceconfig.ParseResult{Config: &sc}
}

func isValidRetryPolicy(jrp *jsonRetryPolicy) bool {
	return jrp.MaxAttempts > 1 &&
		jrp.InitialBackoff > 0 &&
		jrp.MaxBackoff > 0 &&
		jrp.BackoffMultiplier > 0 &&
		len(jrp.RetryableStatusCodes) > 0
}

func convertRetryPolicy(jrp *jsonRetryPolicy, maxAttempts int) (p *internalserviceconfig.RetryPolicy, err error) {
	if jrp == nil {
		return nil, nil
	}

	if !isValidRetryPolicy(jrp) {
		return nil, fmt.Errorf("invalid retry policy (%+v): ", jrp)
	}

	if int64(jrp.MaxAttempts) < int64(maxAttempts) {
		maxAttempts = int(jrp.MaxAttempts)
	}
	rp := &internalserviceconfig.RetryPolicy{
		MaxAttempts:          maxAttempts,
		InitialBackoff:       time.Duration(jrp.InitialBackoff),
		MaxBackoff:           time.Duration(jrp.MaxBackoff),
		BackoffMultiplier:    jrp.BackoffMultiplier,
		RetryableStatusCodes: make(map[codes.Code]bool),
	}
	for _, code := range jrp.RetryableStatusCodes {
		rp.RetryableStatusCodes[code] = true
	}
	return rp, nil
}

func minPointers(a, b *int) *int {
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
		return minPointers(mcMax, doptMax)
	}
	if mcMax != nil {
		return mcMax
	}
	return doptMax
}

func newInt(b int) *int {
	return &b
}

func init() {
	internal.EqualServiceConfigForTesting = equalServiceConfig
}

// equalServiceConfig compares two configs. The rawJSONString field is ignored,
// because they may diff in white spaces.
//
// If any of them is NOT *ServiceConfig, return false.
func equalServiceConfig(a, b serviceconfig.Config) bool {
	if a == nil && b == nil {
		return true
	}
	aa, ok := a.(*ServiceConfig)
	if !ok {
		return false
	}
	bb, ok := b.(*ServiceConfig)
	if !ok {
		return false
	}
	aaRaw := aa.rawJSONString
	aa.rawJSONString = ""
	bbRaw := bb.rawJSONString
	bb.rawJSONString = ""
	defer func() {
		aa.rawJSONString = aaRaw
		bb.rawJSONString = bbRaw
	}()
	// Using reflect.DeepEqual instead of cmp.Equal because many balancer
	// configs are unexported, and cmp.Equal cannot compare unexported fields
	// from unexported structs.
	return reflect.DeepEqual(aa, bb)
}
