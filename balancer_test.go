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

package grpc

import (
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
)

func newTestBalancerBuilder() *testBalancerBuilder {
	return &testBalancerBuilder{
		name: "testBalancer" + strconv.FormatInt(time.Now().UnixNano(), 36),
	}
}

// testBalancerBuilder is a wrapper of pickfirstBuilder. It also stores the
// custom balancer build option specified by DialOptions.
type testBalancerBuilder struct {
	pickfirstBuilder

	name     string
	buildOpt balancer.BuildOptions
}

func (r *testBalancerBuilder) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	r.buildOpt = opts
	return r.pickfirstBuilder.Build(cc, opts)
}
func (r *testBalancerBuilder) Name() string {
	return r.name
}

// Tests that options in WithBalancerUserOptions are passed to balancer.Build().
func TestBalancerUserOptions(t *testing.T) {
	r, cleanup := manual.GenerateAndRegisterManualResolver()
	defer cleanup()

	b := newTestBalancerBuilder()
	balancer.Register(b)

	userOpt := "testUserOpt"
	_, err := Dial(r.Scheme()+":///test.server", WithInsecure(),
		WithBalancerName(b.Name()),
		WithBalancerUserOptions(userOpt),
	)
	if err != nil {
		t.Fatalf("Dial returned error %v, want <nil>", err)
	}

	// An update from resolver will trigger balancer Build().
	r.NewAddress([]resolver.Address{{Addr: "no.such.backend"}})

	for i := 0; i < 1000; i++ {
		if b.buildOpt.UserOptions != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if b.buildOpt.UserOptions != userOpt {
		t.Fatalf("buildOpt.UserOptions = %T %+v, want %v", b.buildOpt.UserOptions, b.buildOpt.UserOptions, userOpt)
	}
}
