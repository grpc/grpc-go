/*
 *
 * Copyright 2022 gRPC authors.
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

package ringhash

import "google.golang.org/grpc/resolver"

// weightAttributeKey is used as the attribute key when storing the address
// weight in the `Attributes` field of the address.
type weightAttributeKey struct{}

// setWeightAttribute returns a copy of addr in which the Attributes field is
// updated with weight.
func setWeightAttribute(addr resolver.Address, weight uint32) resolver.Address {
	addr.Attributes = addr.Attributes.WithValue(weightAttributeKey{}, weight)
	return addr
}

// getWeightAttribute returns the weight stored in the Attributes fields of
// addr.
func getWeightAttribute(addr resolver.Address) uint32 {
	v := addr.Attributes.Value(weightAttributeKey{})
	weight, _ := v.(uint32)
	return weight
}
