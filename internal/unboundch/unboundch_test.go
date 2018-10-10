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

package unboundch

import (
	"testing"
)

func TestUnboundChannel(t *testing.T) {
	uc := NewUnboundChannel(0)
	// put datas
	for i := 1; i <= 100; i++ {
		uc.Put(i)
	}

	datas := make([]int, 0, 100)
	// get datas
	for i := 1; i <= 100; i++ {
		d := <-uc.Get()
		uc.Load()
		datas = append(datas, d.(int))
	}

	// check datas
	for i := 0; i < 100; i++ {
		if datas[i] != i+1 {
			t.Errorf("check data failed! i=%d", i)
			t.FailNow()
		}
	}
}
