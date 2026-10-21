/*
 *
 * Copyright 2026 gRPC authors.
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

package credentials_test

import (
	"testing"

	xdscreds "google.golang.org/grpc/internal/xds/credentials"
)

func (s) TestCredentialConfigEquality(t *testing.T) {
	id := &xdscreds.Identity{Type: "test", Data: []byte("config")}
	tests := []struct {
		name string
		a, b *xdscreds.Identity
		want bool
	}{
		{name: "both_nil", want: true},
		{name: "first_nil", b: id},
		{name: "second_nil", a: id},
		{name: "equal", a: id, b: id, want: true},
		{name: "different_type", a: id, b: &xdscreds.Identity{Type: "other", Data: id.Data}},
		{name: "different_data", a: id, b: &xdscreds.Identity{Type: id.Type, Data: []byte("other")}},
		{name: "nil_and_empty_data", a: &xdscreds.Identity{Type: "test"}, b: &xdscreds.Identity{Type: "test", Data: []byte{}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var channels [2]*xdscreds.ChannelCreds
			var calls [2]*xdscreds.CallCreds
			var buildableChannels [2]xdscreds.BuildableChannelCreds
			var buildableCalls [2]xdscreds.BuildableCallCreds
			for i, identity := range []*xdscreds.Identity{test.a, test.b} {
				if identity != nil {
					channels[i] = xdscreds.NewChannelCreds(nil, *identity)
					calls[i] = xdscreds.NewCallCreds(nil, *identity)
					buildableChannels[i] = channels[i]
					buildableCalls[i] = calls[i]
				}
			}
			if got := channels[0].Equal(channels[1]); got != test.want {
				t.Errorf("ChannelCreds.Equal() = %v, want %v", got, test.want)
			}
			if got := calls[0].Equal(calls[1]); got != test.want {
				t.Errorf("CallCreds.Equal() = %v, want %v", got, test.want)
			}
			if got := xdscreds.EqualChannelCreds(buildableChannels[0], buildableChannels[1]); got != test.want {
				t.Errorf("EqualChannelCreds() = %v, want %v", got, test.want)
			}
			if got := xdscreds.EqualCallCreds(buildableCalls[0], buildableCalls[1]); got != test.want {
				t.Errorf("EqualCallCreds() = %v, want %v", got, test.want)
			}
		})
	}
}
