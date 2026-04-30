/*
 *
 * Copyright 2026 gRPC authors.
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

package extproc

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/xds/httpfilter"
	"google.golang.org/grpc/internal/xds/matcher"
	"google.golang.org/grpc/metadata"
)

const testBaseURI = "base-uri"

// incorrectFilterConfig embeds httpfilter.FilterConfig but is not of type
// baseConfig/overrideConfig, and is used to test incorrect config types being
// passed to BuildClientInterceptor.
type incorrectFilterConfig struct {
	httpfilter.FilterConfig
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestBuildClientInterceptor(t *testing.T) {

	b := builder{}
	f := b.BuildClientFilter()
	defer f.Close()

	tests := []struct {
		name       string
		cfg        httpfilter.FilterConfig
		override   httpfilter.FilterConfig
		wantConfig *interceptorConfig
		wantErr    string
	}{
		{
			name:    "NilConfig",
			cfg:     nil,
			wantErr: "extproc: nil config provided",
		},
		{
			name:    "IncorrectConfigType",
			cfg:     incorrectFilterConfig{},
			wantErr: "extproc: incorrect config type provided",
		},
		{
			name:     "IncorrectOverrideType",
			cfg:      baseConfig{config: interceptorConfig{}},
			override: incorrectFilterConfig{},
			wantErr:  "extproc: incorrect override config type provided",
		},
		{
			name: "ConfigUsingOnlyBase",
			cfg: baseConfig{
				config: interceptorConfig{
					failureModeAllow:         func() *bool { b := true; return &b }(),
					requestAttributes:        []string{"attr1"},
					responseAttributes:       []string{"attr2"},
					observabilityMode:        true,
					disableImmediateResponse: true,
					deferredCloseTimeout:     10 * time.Second,
					processingModes: &processingModes{
						requestHeaderMode:   modeSend,
						responseHeaderMode:  modeSkip,
						responseTrailerMode: modeSend,
						requestBodyMode:     modeSend,
						responseBodyMode:    modeSkip,
					},
					server: &serverConfig{
						targetURI:          testBaseURI,
						channelCredentials: insecure.NewCredentials(),
					},
					mutationRules: headerMutationRules{
						allowExpr:       regexp.MustCompile("allow-.*"),
						disallowExpr:    regexp.MustCompile("disallow-.*"),
						disallowAll:     true,
						disallowIsError: true,
					},
					allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				},
			},
			wantConfig: &interceptorConfig{
				failureModeAllow:   func() *bool { b := true; return &b }(),
				requestAttributes:  []string{"attr1"},
				responseAttributes: []string{"attr2"},
				mutationRules: headerMutationRules{
					allowExpr:       regexp.MustCompile("allow-.*"),
					disallowExpr:    regexp.MustCompile("disallow-.*"),
					disallowAll:     true,
					disallowIsError: true,
				},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: &processingModes{
					requestHeaderMode:   modeSend,
					responseHeaderMode:  modeSkip,
					responseTrailerMode: modeSend,
					requestBodyMode:     modeSend,
					responseBodyMode:    modeSkip,
				},
				server: &serverConfig{
					targetURI:          testBaseURI,
					channelCredentials: insecure.NewCredentials(),
				},
				allowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
			},
		},
		{
			name: "ConfigUsingBaseAndOverride",
			cfg: baseConfig{
				config: interceptorConfig{
					failureModeAllow:         func() *bool { b := false; return &b }(),
					requestAttributes:        []string{"base-attr1"},
					responseAttributes:       []string{"base-attr2"},
					observabilityMode:        true,
					disableImmediateResponse: true,
					deferredCloseTimeout:     10 * time.Second,
					processingModes: &processingModes{
						requestHeaderMode:   modeSend,
						responseHeaderMode:  modeSkip,
						responseTrailerMode: modeSend,
						requestBodyMode:     modeSend,
						responseBodyMode:    modeSkip,
					},
					server: &serverConfig{
						targetURI:          testBaseURI,
						channelCredentials: insecure.NewCredentials(),
						timeout:            time.Second,
						initialMetadata:    metadata.MD(metadata.Pairs("key1", "value1")),
					},
					mutationRules: headerMutationRules{
						allowExpr:       regexp.MustCompile("allow-.*"),
						disallowExpr:    regexp.MustCompile("disallow-.*"),
						disallowAll:     true,
						disallowIsError: true,
					},
					allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
					disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
				},
			},
			override: overrideConfig{
				config: interceptorConfig{
					failureModeAllow:   func() *bool { b := true; return &b }(),
					requestAttributes:  []string{"override-attr1"},
					responseAttributes: []string{"override-attr2"},
					processingModes: &processingModes{
						requestHeaderMode:   modeSkip,
						responseHeaderMode:  modeSend,
						responseTrailerMode: modeSkip,
						requestBodyMode:     modeSkip,
						responseBodyMode:    modeSend,
					},
					server: &serverConfig{
						targetURI:          "override-uri",
						channelCredentials: insecure.NewCredentials(),
					},
				},
			},
			wantConfig: &interceptorConfig{
				failureModeAllow:   func() *bool { b := true; return &b }(),
				requestAttributes:  []string{"override-attr1"},
				responseAttributes: []string{"override-attr2"},
				mutationRules: headerMutationRules{
					allowExpr:       regexp.MustCompile("allow-.*"),
					disallowExpr:    regexp.MustCompile("disallow-.*"),
					disallowAll:     true,
					disallowIsError: true,
				},
				observabilityMode:        true,
				disableImmediateResponse: true,
				deferredCloseTimeout:     10 * time.Second,
				processingModes: &processingModes{
					requestHeaderMode:   modeSkip,
					responseHeaderMode:  modeSend,
					responseTrailerMode: modeSkip,
					requestBodyMode:     modeSkip,
					responseBodyMode:    modeSend,
				},
				server: &serverConfig{
					targetURI:          "override-uri",
					channelCredentials: insecure.NewCredentials(),
				},
				allowedHeaders:    []matcher.StringMatcher{matcher.NewExactStringMatcher("allow-header", false)},
				disallowedHeaders: []matcher.StringMatcher{matcher.NewExactStringMatcher("disallow-header", false)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			intptr, err := f.BuildClientInterceptor(tc.cfg, tc.override)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("BuildClientInterceptor() returned unexpected error: %v", err)
				}
				ic, ok := intptr.(*interceptor)
				if !ok {
					t.Fatalf("BuildClientInterceptor() returned %T, want *interceptor", intptr)
				}
				cmpOpts := []cmp.Option{
					cmp.AllowUnexported(interceptorConfig{}, serverConfig{}, processingModes{}, headerMutationRules{}),
					cmp.Transformer("RegexpToString", func(r *regexp.Regexp) string {
						if r == nil {
							return ""
						}
						return r.String()
					}),
					cmp.Comparer(func(x, y matcher.StringMatcher) bool {
						return x.Equal(y)
					}),
				}
				if diff := cmp.Diff(ic.config, *tc.wantConfig, cmpOpts...); diff != "" {
					t.Fatalf("Interceptor config returned unexpected diff (-got +want):\n%s", diff)
				}
				intptr.Close()
				return
			}
			if err == nil {
				t.Fatalf("BuildClientInterceptor() returned nil error, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("BuildClientInterceptor() error = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}
