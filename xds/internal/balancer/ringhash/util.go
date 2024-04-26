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
	"strings"

	"github.com/cespare/xxhash/v2"
	"google.golang.org/grpc/metadata"
)

type xdsHashKey struct{}

// getRequestHash returns the request hash to use for this pick, and whether
// a random hash was used.
func getRequestHash(ctx context.Context, requestMetadataKey string) (uint64, bool) {
	if requestMetadataKey == "" {
		// No explicit request metadata key, use the hash set by the xDS
		// resolver.
		requestHash, _ := ctx.Value(xdsHashKey{}).(uint64)
		return requestHash, false
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	values := md.Get(requestMetadataKey)
	if len(values) == 0 || len(values) == 1 && values[0] == "" {
		// If the header is not present, generate a random hash.
		return 0, true
	}
	joinedValues := strings.Join(values, ",")
	return xxhash.Sum64String(joinedValues), false
}

// GetXDSRequestHashForTesting returns the request hash in the context; to be used
// for testing only.
func GetXDSRequestHashForTesting(ctx context.Context) uint64 {
	// for xDS the random hash is never generated in the picker.
	h, _ := getRequestHash(ctx, "")
	return h
}

// SetXDSRequestHash adds the request hash to the context for use in Ring Hash
// Load Balancing using xDS route hash_policy.
func SetXDSRequestHash(ctx context.Context, requestHash uint64) context.Context {
	return context.WithValue(ctx, xdsHashKey{}, requestHash)
}
