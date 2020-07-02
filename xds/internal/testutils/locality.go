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
 */

package testutils

import (
	corepb "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"google.golang.org/grpc/xds/internal"
)

// LocalityIDToProto converts a LocalityID to its proto representation.
func LocalityIDToProto(l internal.LocalityID) *corepb.Locality {
	return &corepb.Locality{
		Region:  l.Region,
		Zone:    l.Zone,
		SubZone: l.SubZone,
	}
}
