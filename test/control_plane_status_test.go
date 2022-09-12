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

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip"
	iresolver "google.golang.org/grpc/internal/resolver"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"
	testpb "google.golang.org/grpc/test/grpc_testing"
)

func (s) TestConfigSelectorStatusCodes(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	ss.R = manual.NewBuilderWithScheme("confSel")

	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	csErr := make(chan error, 1)
	state := iresolver.SetConfigSelector(resolver.State{
		Addresses:     []resolver.Address{{Addr: ss.Address}},
		ServiceConfig: parseServiceConfig(t, ss.R, "{}"),
	}, funcConfigSelector{
		f: func(i iresolver.RPCInfo) (*iresolver.RPCConfig, error) {
			return nil, <-csErr
		},
	})
	ss.R.UpdateState(state) // Blocks until config selector is applied

	testCases := []struct {
		name  string
		csErr error
		want  error
	}{{
		name:  "legal status code",
		csErr: status.Errorf(codes.Unavailable, "this error is fine"),
		want:  status.Errorf(codes.Unavailable, "this error is fine"),
	}, {
		name:  "illegal status code",
		csErr: status.Errorf(codes.NotFound, "this error is bad"),
		want:  status.Errorf(codes.Internal, "this error is bad"),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// In case the channel is full due to a previous iteration failure,
			// do not block.
			select {
			case csErr <- tc.csErr:
			default:
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != status.Code(tc.want) || !strings.Contains(err.Error(), status.Convert(tc.want).Message()) {
				t.Fatalf("client.EmptyCall(_, _) = _, %v; want _, %v", err, tc.want)
			}
		})
	}
}

type lbBuilderWrapper struct {
	builder balancer.Builder // real Builder
	name    string
	picker  func(balancer.PickInfo) error
}

func (l *lbBuilderWrapper) Build(cc balancer.ClientConn, opts balancer.BuildOptions) balancer.Balancer {
	return l.builder.Build(&lbCCWrapper{ClientConn: cc, picker: l.picker}, opts)
}

func (l *lbBuilderWrapper) Name() string {
	return l.name
}

type lbCCWrapper struct {
	balancer.ClientConn // real ClientConn
	picker              func(balancer.PickInfo) error
}

func (l *lbCCWrapper) UpdateState(s balancer.State) {
	s.Picker = &lbPickerWrapper{picker: l.picker, Picker: s.Picker}
	l.ClientConn.UpdateState(s)
}

type lbPickerWrapper struct {
	balancer.Picker // real Picker
	picker          func(balancer.PickInfo) error
}

func (lp *lbPickerWrapper) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	if err := lp.picker(info); err != nil {
		return balancer.PickResult{}, err
	}
	return lp.Picker.Pick(info)
}

func (s) TestPickerStatusCodes(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	pickerErr := make(chan error, 1)
	balancer.Register(&lbBuilderWrapper{
		builder: balancer.Get("round_robin"),
		name:    "testPickerStatusCodesBalancer",
		picker: func(balancer.PickInfo) error {
			return <-pickerErr
		},
	})

	ss.NewServiceConfig(`{"loadBalancingConfig": [{"testPickerStatusCodesBalancer":{}}] }`)

	// Make calls until pickerErr is used.
	pickerErr <- errors.New("err")
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	used := false
	for !used {
		ss.Client.EmptyCall(ctx, &testpb.Empty{})
		select {
		case pickerErr <- errors.New("err"):
			<-pickerErr
			used = true
		default:
		}
	}

	testCases := []struct {
		name      string
		pickerErr error
		want      error
	}{{
		name:      "legal status code",
		pickerErr: status.Errorf(codes.Unavailable, "this error is fine"),
		want:      status.Errorf(codes.Unavailable, "this error is fine"),
	}, {
		name:      "illegal status code",
		pickerErr: status.Errorf(codes.NotFound, "this error is bad"),
		want:      status.Errorf(codes.Internal, "this error is bad"),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// In case the channel is full due to a previous iteration failure,
			// do not block.
			select {
			case pickerErr <- tc.pickerErr:
			default:
			}

			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != status.Code(tc.want) || !strings.Contains(err.Error(), status.Convert(tc.want).Message()) {
				t.Fatalf("client.EmptyCall(_, _) = _, %v; want _, %v", err, tc.want)
			}
		})
	}
}

func (s) TestCallCredsFromDialOptionsStatusCodes(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	errChan := make(chan error, 1)
	creds := &testPerRPCCredentials{errChan: errChan}

	if err := ss.Start(nil, grpc.WithPerRPCCredentials(creds)); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testCases := []struct {
		name     string
		credsErr error
		want     error
	}{{
		name:     "legal status code",
		credsErr: status.Errorf(codes.Unavailable, "this error is fine"),
		want:     status.Errorf(codes.Unavailable, "this error is fine"),
	}, {
		name:     "illegal status code",
		credsErr: status.Errorf(codes.NotFound, "this error is bad"),
		want:     status.Errorf(codes.Internal, "this error is bad"),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// In case the channel is full due to a previous iteration failure,
			// do not block.
			select {
			case errChan <- tc.credsErr:
			default:
			}

			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != status.Code(tc.want) || !strings.Contains(err.Error(), status.Convert(tc.want).Message()) {
				t.Fatalf("client.EmptyCall(_, _) = _, %v; want _, %v", err, tc.want)
			}
		})
	}
}

func (s) TestCallCredsFromCallOptionsStatusCodes(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	errChan := make(chan error, 1)
	creds := &testPerRPCCredentials{errChan: errChan}

	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	testCases := []struct {
		name     string
		credsErr error
		want     error
	}{{
		name:     "legal status code",
		credsErr: status.Errorf(codes.Unavailable, "this error is fine"),
		want:     status.Errorf(codes.Unavailable, "this error is fine"),
	}, {
		name:     "illegal status code",
		credsErr: status.Errorf(codes.NotFound, "this error is bad"),
		want:     status.Errorf(codes.Internal, "this error is bad"),
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// In case the channel is full due to a previous iteration failure,
			// do not block.
			select {
			case errChan <- tc.credsErr:
			default:
			}

			if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.PerRPCCredentials(creds)); status.Code(err) != status.Code(tc.want) || !strings.Contains(err.Error(), status.Convert(tc.want).Message()) {
				t.Fatalf("client.EmptyCall(_, _) = _, %v; want _, %v", err, tc.want)
			}
		})
	}
}
