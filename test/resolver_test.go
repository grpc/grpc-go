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

package test

import (
	"context"
	"reflect"
	"testing"
	"time"

	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/serviceconfig"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

type funcConfigSelector struct {
	f func(iresolver.RPCInfo) *iresolver.RPCConfig
}

func (f funcConfigSelector) SelectConfig(i iresolver.RPCInfo) *iresolver.RPCConfig {
	return f.f(i)
}

func (s) TestConfigSelector(t *testing.T) {
	gotInfoChan := make(chan *iresolver.RPCInfo, 1)
	sendConfigChan := make(chan *iresolver.RPCConfig, 1)
	gotContextChan := make(chan context.Context, 1)

	ss := &stubServer{
		emptyCall: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			gotContextChan <- ctx
			return &testpb.Empty{}, nil
		},
	}
	ss.r = manual.NewBuilderWithScheme("confSel")

	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	state := iresolver.SetConfigSelector(resolver.State{
		Addresses:     []resolver.Address{{Addr: ss.address}},
		ServiceConfig: parseCfg(ss.r, "{}"),
	}, funcConfigSelector{
		f: func(i iresolver.RPCInfo) *iresolver.RPCConfig {
			gotInfoChan <- &i
			cfg := <-sendConfigChan
			if cfg != nil && cfg.Context == nil {
				cfg.Context = i.Context
			}
			return cfg
		},
	})
	ss.r.UpdateState(state) // Blocks until config selector is applied

	longdeadlineCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shorterTimeout := 3 * time.Second

	testMD := metadata.MD{"footest": []string{"bazbar"}}
	mdOut := metadata.MD{"handler": []string{"value"}}

	var onCommittedCalled bool

	testCases := []struct {
		name   string
		md     metadata.MD
		config *iresolver.RPCConfig
	}{{
		name:   "basic",
		md:     testMD,
		config: &iresolver.RPCConfig{},
	}, {
		name: "alter MD",
		md:   testMD,
		config: &iresolver.RPCConfig{
			Context: metadata.NewOutgoingContext(ctx, mdOut),
		},
	}, {
		name: "alter timeout; remove MD",
		md:   testMD,
		config: &iresolver.RPCConfig{
			Context: longdeadlineCtx,
		},
	}, {
		name:   "nil config",
		md:     metadata.MD{},
		config: nil,
	}, {
		name: "alter timeout via method config; remove MD",
		md:   testMD,
		config: &iresolver.RPCConfig{
			MethodConfig: serviceconfig.MethodConfig{
				Timeout: &shorterTimeout,
			},
		},
	}, {
		name: "onCommitted callback",
		md:   testMD,
		config: &iresolver.RPCConfig{
			OnCommitted: func() {
				onCommittedCalled = true
			},
		},
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			select {
			case sendConfigChan <- tc.config:
			default:
				t.Fatalf("last config not consumed by config selector")
			}

			onCommittedCalled = false
			ctx = metadata.NewOutgoingContext(ctx, tc.md)
			if _, err := ss.client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
				t.Fatalf("client.EmptyCall(_, _) = _, %v; want _, nil", err)
			}

			var gotInfo *iresolver.RPCInfo
			select {
			case gotInfo = <-gotInfoChan:
			default:
				t.Fatalf("no config selector data")
			}

			var gotContext context.Context
			select {
			case gotContext = <-gotContextChan:
			default:
				t.Fatalf("no context received")
			}

			if want := "/grpc.testing.TestService/EmptyCall"; gotInfo.Method != want {
				t.Errorf("gotInfo.Method = %q; want %q", gotInfo.Method, want)
			}

			if gotMD, _ := metadata.FromOutgoingContext(gotInfo.Context); !reflect.DeepEqual(tc.md, gotMD) {
				t.Errorf("gotInfo.Context contains MD %v; want %v", gotMD, tc.md)
			}

			sentMD, _ := metadata.FromOutgoingContext(ctx)
			if tc.config != nil && tc.config.Context != nil {
				sentMD, _ = metadata.FromOutgoingContext(tc.config.Context)
			}
			gotMD, _ := metadata.FromIncomingContext(gotContext)
			for k, v := range sentMD {
				if !reflect.DeepEqual(gotMD[k], v) {
					t.Errorf("received md = %v; want contains(%v)", gotMD, sentMD)
				}
			}

			deadlineSent, _ := ctx.Deadline()
			if tc.config != nil && tc.config.Context != nil {
				deadlineSent, _ = tc.config.Context.Deadline()
			}
			if tc.config != nil && tc.config.MethodConfig.Timeout != nil && *tc.config.MethodConfig.Timeout < time.Until(deadlineSent) {
				deadlineSent = time.Now().Add(*tc.config.MethodConfig.Timeout)
			}
			deadlineGot, _ := gotContext.Deadline()
			if diff := deadlineGot.Sub(deadlineSent); diff > time.Second || diff < -time.Second {
				t.Errorf("received deadline = %v; want ~%v", deadlineGot, deadlineSent)
			}

			if tc.config != nil && tc.config.OnCommitted != nil && !onCommittedCalled {
				t.Errorf("OnCommitted callback not called")
			}
		})
	}

}
