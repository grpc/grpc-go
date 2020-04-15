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

package weightedroundrobin

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"google.golang.org/grpc/attributes"
)

func TestAddAddrInfoToAndFromAttributes(t *testing.T) {
	tests := []struct {
		desc            string
		inputAddrInfo   *AddrInfo
		inputAttributes *attributes.Attributes
		wantAddrInfo    *AddrInfo
	}{
		{
			desc:            "empty attributes",
			inputAddrInfo:   &AddrInfo{Weight: 100},
			inputAttributes: nil,
			wantAddrInfo:    &AddrInfo{Weight: 100},
		},
		{
			desc:            "non-empty attributes",
			inputAddrInfo:   &AddrInfo{Weight: 100},
			inputAttributes: attributes.New("foo", "bar"),
			wantAddrInfo:    &AddrInfo{Weight: 100},
		},
		{
			desc:            "addrInfo not present in empty attributes",
			inputAddrInfo:   nil,
			inputAttributes: nil,
			wantAddrInfo:    nil,
		},
		{
			desc:            "addrInfo not present in non-empty attributes",
			inputAddrInfo:   nil,
			inputAttributes: attributes.New("foo", "bar"),
			wantAddrInfo:    nil,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			outputAttributes := AddAddrInfoToAttributes(test.inputAddrInfo, test.inputAttributes)
			gotAddrInfo := GetAddrInfoFromAttributes(outputAttributes)
			if !cmp.Equal(gotAddrInfo, test.wantAddrInfo) {
				t.Errorf("gotAddrInfo: %v, wantAddrInfo: %v", gotAddrInfo, test.wantAddrInfo)
			}

		})
	}
}
