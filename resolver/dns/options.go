/*
 *
 * Copyright 2018 gRPC authors.
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

package dns

import (
	"time"
)

// builderOptions configure the dns resolver builder. builderOptions are set by the BuilderOption
// values passed to NewBuilder.
type builderOptions struct {
	minFreq time.Duration
}

// BuilderOption configures how we build the dns resolver.
type BuilderOption interface {
	apply(*builderOptions)
}

// funcBuilderOption wraps a function that modifies builderOptions into an
// implementation of the BuilderOption interface.
type funcBuilderOption struct {
	f func(*builderOptions)
}

func (fbo *funcBuilderOption) apply(opts *builderOptions) {
	fbo.f(opts)
}

func newFuncBuilderOption(f func(*builderOptions)) *funcBuilderOption {
	return &funcBuilderOption{
		f: f,
	}
}

// WithFreq sets the minimum frequency of polling the DNS server.
func WithFreq(freq time.Duration) BuilderOption {
	return newFuncBuilderOption(func(o *builderOptions) {
		o.minFreq = freq
	})
}

func defaultBuilderOptions() builderOptions {
	return builderOptions{
		// minimum frequency of polling the DNS server.
		minFreq: defaultFreq,
	}
}
