/*
 *
 * Copyright 2019 gRPC authors.
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

package backoff

import (
	"testing"
	"time"
)

func TestNewExponentialBuilderDefault(t *testing.T) {
	gotExp := NewExponentialBuilder().Build().(Strategy)
	wantExp := Exponential{
		baseDelay:         baseDelay,
		multiplier:        multiplier,
		jitter:            jitter,
		maxDelay:          maxDelay,
		minConnectTimeout: minConnectTimeout,
	}
	if gotExp != wantExp {
		t.Errorf("DefaultExponentialBuilder got: %v, want %v", gotExp, wantExp)
	}
}

func TestNewExponentialBuilderOverride(t *testing.T) {
	bd := 2 * time.Second
	mltpr := 2.0
	gotExp := NewExponentialBuilder().BaseDelay(bd).Multiplier(mltpr).Build().(Strategy)
	wantExp := Exponential{
		baseDelay:         bd,
		multiplier:        mltpr,
		jitter:            jitter,
		maxDelay:          maxDelay,
		minConnectTimeout: minConnectTimeout,
	}
	if gotExp != wantExp {
		t.Errorf("OverrideExponentialBuilder got: %v, want %v", gotExp, wantExp)
	}
}
