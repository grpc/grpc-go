// +build appengine

/*
 *
 * Copyright 2020 gRPC authors.
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

package resolver

import (
	"sync"
)

// SafeConfigSelector allows for safe switching of ConfigSelector
// implementations such that previous values are guaranteed to not be in use
// when UpdateConfigSelector returns.
type SafeConfigSelector struct {
	mu  sync.Mutex
	ccs *countingConfigSelector // current config selector
}

// UpdateConfigSelector swaps to the provided ConfigSelector and blocks until
// all uses of the previous ConfigSelector have completed.
func (scs *SafeConfigSelector) UpdateConfigSelector(cs ConfigSelector) {
	scs.mu.Lock()
	oldCCS := scs.ccs
	scs.ccs = &countingConfigSelector{ConfigSelector: cs}
	scs.mu.Unlock()
	if oldCCS != nil {
		oldCCS.Wait()
	}
}

// SelectConfig defers to the current ConfigSelector in scs.
func (scs *SafeConfigSelector) SelectConfig(r RPCInfo) *RPCConfig {
	scs.mu.Lock()
	ccs := scs.ccs
	ccs.Add()
	scs.mu.Unlock()
	defer ccs.Done()
	return ccs.SelectConfig(r)
}
