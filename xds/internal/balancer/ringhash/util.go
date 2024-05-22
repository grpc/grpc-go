/*
 *
 * Copyright 2021 gRPC authors.
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

import (
	"context"
)

type xdsHashKey struct{}

// GetXDSRequestHash returns the request hash in the context, set from the
// xDS config selector.
func GetXDSRequestHash(ctx context.Context) uint64 {
	requestHash, _ := ctx.Value(xdsHashKey{}).(uint64)
	return requestHash
}

// SetXDSRequestHash adds the request hash to the context for use in Ring Hash
// Load Balancing using xDS route hash_policy.
func SetXDSRequestHash(ctx context.Context, requestHash uint64) context.Context {
	return context.WithValue(ctx, xdsHashKey{}, requestHash)
}
