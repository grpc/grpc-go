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

package grpc

import (
	"testing"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal"
)

func (s) TestSetDefaultDialOptions(t *testing.T) {
	opts := []DialOption{WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials())}
	internal.SetDefaultDialOption.(func(opt ...DialOption))(opts...)
	for i, opt := range opts {
		if defaultDialOption[i] != opt {
			t.Fatalf("Unexpected default dial option at index %d: %v != %v", i, defaultDialOption[i], opt)
		}
	}
	internal.SetDefaultDialOption.(func(opt ...DialOption))()
	if len(defaultDialOption) != 0 {
		t.Fatalf("Unexpected len of defaultDialOption: %d != 0", len(defaultDialOption))
	}
}

func (s) TestSetDefaultServerOptions(t *testing.T) {
	opts := []ServerOption{StatsHandler(nil), Creds(insecure.NewCredentials()), MaxRecvMsgSize(1024)}
	internal.SetDefaultServerOption.(func(opt ...ServerOption))(opts...)
	for i, opt := range opts {
		if defaultServerOption[i] != opt {
			t.Fatalf("Unexpected default server option at index %d: %v != %v", i, defaultServerOption[i], opt)
		}
	}
	internal.SetDefaultServerOption.(func(opt ...ServerOption))()
	if len(defaultServerOption) != 0 {
		t.Fatalf("Unexpected len of defaultServerOption: %d != 0", len(defaultServerOption))
	}
}
