// +build !appengine

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
	"sync/atomic"
	"unsafe"
)

// SafeConfigSelector allows for safe switching of ConfigSelector
// implementations such that previous values are guaranteed to not be in use
// when UpdateConfigSelector returns.
type SafeConfigSelector struct {
	ccs unsafe.Pointer // *countingConfigSelector: current config selector
}

// UpdateConfigSelector swaps to the provided ConfigSelector and blocks until
// all uses of the previous ConfigSelector have completed.
func (scs *SafeConfigSelector) UpdateConfigSelector(cs ConfigSelector) {
	newCCS := &countingConfigSelector{ConfigSelector: cs}
	oldCCSPtr := atomic.SwapPointer(&scs.ccs, unsafe.Pointer(newCCS))
	if oldCCSPtr == nil {
		return
	}
	(*countingConfigSelector)(oldCCSPtr).wg.Wait()
}

// SelectConfig defers to the current ConfigSelector in scs.
func (scs *SafeConfigSelector) SelectConfig(r RPCInfo) *RPCConfig {
	ccsPtr := atomic.LoadPointer(&scs.ccs)
	var ccs *countingConfigSelector
	for {
		if ccsPtr == nil {
			return nil
		}
		ccs = (*countingConfigSelector)(ccsPtr)
		ccs.wg.Add(1)
		ccsPtr2 := atomic.LoadPointer(&scs.ccs)
		if ccsPtr == ccsPtr2 {
			// Use ccs with confidence!
			break
		}
		// ccs changed; try to use the new one instead, because the old one is
		// no longer valid to use.
		ccs.wg.Done()
		ccsPtr = ccsPtr2
	}
	defer ccs.wg.Done()
	return ccs.SelectConfig(r)
}
