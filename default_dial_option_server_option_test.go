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

func (s) TestAddDefaultDialOptions(t *testing.T) {
	opts := []DialOption{WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials()), WithTransportCredentials(insecure.NewCredentials())}
	internal.AddDefaultDialOptions.(func(opt ...DialOption))(opts...)
	for i, opt := range opts {
		if extraDialOption[i] != opt {
			t.Fatalf("Unexpected default dial option at index %d: %v != %v", i, extraDialOption[i], opt)
		}
	}
	internal.ClearDefaultDialOptions()
	if len(extraDialOption) != 0 {
		t.Fatalf("Unexpected len of extraDialOption: %d != 0", len(extraDialOption))
	}
}

func (s) TestAddDefaultServerOptions(t *testing.T) {
	opts := []ServerOption{StatsHandler(nil), Creds(insecure.NewCredentials()), MaxRecvMsgSize(1024)}
	internal.AddDefaultServerOptions.(func(opt ...ServerOption))(opts...)
	for i, opt := range opts {
		if extraServerOption[i] != opt {
			t.Fatalf("Unexpected default server option at index %d: %v != %v", i, extraServerOption[i], opt)
		}
	}
	internal.ClearDefaultServerOptions()
	if len(extraServerOption) != 0 {
		t.Fatalf("Unexpected len of extraServerOption: %d != 0", len(extraServerOption))
	}
}
