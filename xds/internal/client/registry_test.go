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

package client

import "testing"

func TestAddGetRemove(t *testing.T) {
	cl := &Client{}
	cr := newClientRegistry()

	key := cr.add(cl)
	got, err := cr.get(key)
	if err != nil {
		t.Error(err)
	}
	if got != cl {
		t.Errorf("get(%v) = %v, want %v", key, got, cl)
	}

	cr.remove(key)
	got, err = cr.get(key)
	if err == nil {
		t.Errorf("get(%v) returned non-nil error", key)
	}
}
