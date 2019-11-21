/*
 *
 * Copyright 2019 gRPC authors.
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

package httpovergrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	pb "google.golang.org/grpc/httpovergrpc/http_over_grpc_proto"
	"google.golang.org/grpc/status"
)

const (
	port = 10000
)

func TestReadResponse(t *testing.T) {
	testCases := []struct {
		name        string
		resp        []byte
		expectedErr bool
	}{
		{
			"success",
			[]byte(
				"HTTP/1.1 200 OK\r\n" +
					"Content-Type: \"text/html\"\r\n" +
					"Content-Length: 1024\r\n\r\n" +
					"the body"),
			false,
		},
		{
			"bad mime header",
			[]byte("HTTP/1.1 200 OK\r\n\tfoo\r\n\r\n"),
			true,
		},
		{
			"unexpected eof",
			nil,
			true,
		},
	}
	for _, tt := range testCases {
		pr, pw := io.Pipe()
		go func() {
			pw.Write(tt.resp)
		}()

		// If the response is nil, close the writer to produce EOF.
		if tt.resp == nil {
			pw.Close()
			pr.Close()
		}
		req, _ := http.NewRequest("GET", "http://someplace/undefined", nil)
		_, err := readResponse(pr, req)
		if err != nil && !tt.expectedErr {
			t.Errorf("[%s] got error %v, want nil", tt.name, err)
		}
		if err == nil && tt.expectedErr {
			t.Errorf("[%s] got nil, want error", tt.name)
		}

		// Ensure if an error was emitted that the pipe was closed.
		if tt.expectedErr {
			_, perr := pw.Write([]byte("foo"))
			if got, want := perr, err; got != want {
				t.Errorf("perr: got: %v want: %v", got, want)
			}
		}
	}
}

type respOrStatus struct {
	resp   *pb.HTTPOverGRPCReply
	status *status.Status
}

type fakeClient struct {
	cc             *grpc.ClientConn
	respOrStatuses []*respOrStatus
}

func (f *fakeClient) HTTPRequest(ctx context.Context, in *pb.HTTPOverGRPCRequest, opts ...grpc.CallOption) (*pb.HTTPOverGRPCReply, error) {
	var r *respOrStatus
	if len(f.respOrStatuses) > 0 {
		r, f.respOrStatuses = f.respOrStatuses[0], f.respOrStatuses[1:]
		return r.resp, r.status.Err()
	}
	return nil, io.EOF
}

type testClient struct {
	client pb.HTTPOverGRPCClient
}

func TestTransport(t *testing.T) {
	host := fmt.Sprintf("localhost:%d", port)
	conn, err := grpc.Dial(host, grpc.WithInsecure())
	if err != nil {
		grpclog.Infof("grpc dial fail with err = %v", err)
	}
	tr := NewBackgroundTransport(context.Background(), conn)

	req, err := http.NewRequest("GET", "http://invalid-backend", nil)
	if err != nil {
		t.Errorf("failed to create http request: %v", err)
	}
	_, err = tr.RoundTrip(req)

	gs, ok := status.FromError(err)
	if gs.Code() != codes.Unavailable {
		t.Errorf("Unexpected error: %v , expected error: %v , ok = %v", err, gs.Code(), ok)
	}
}

func TestRoundTrip(t *testing.T) {
	testCases := []struct {
		name   string
		client pb.HTTPOverGRPCClient
		err    error
	}{
		{
			"retryable with deadline exceeded",
			&fakeClient{
				respOrStatuses: []*respOrStatus{
					{
						resp:   nil,
						status: status.New(codes.DeadlineExceeded, ""),
					},
				},
			},
			status.Error(codes.DeadlineExceeded, ""),
		},
		{
			"retryable with deadlines and final unreachable",
			&fakeClient{
				respOrStatuses: []*respOrStatus{
					{
						resp:   nil,
						status: status.New(codes.DeadlineExceeded, ""),
					},
				},
			},
			status.Error(codes.DeadlineExceeded, ""),
		},
		{
			"retryable with unreachable",
			&fakeClient{
				respOrStatuses: []*respOrStatus{
					{
						resp:   nil,
						status: status.New(codes.Unavailable, ""),
					},
				},
			},
			status.Error(codes.Unavailable, ""),
		},
		{
			"non-retryable with stream broken",
			&fakeClient{
				respOrStatuses: []*respOrStatus{
					{
						resp:   nil,
						status: status.New(codes.Internal, ""),
					},
				},
			},
			status.Error(codes.Internal, ""),
		},
	}
	for _, tt := range testCases {
		host := fmt.Sprintf("localhost:%d", port)
		conn, err := grpc.Dial(host, grpc.WithInsecure())
		if err != nil {
			grpclog.Infof("grpc dial fail with err = %v", err)
		}
		tr := Transport{context.Background(), conn, tt.client}
		req, err := http.NewRequest("GET", "http://invalid-site", nil)
		if err != nil {
			t.Errorf("[%s] failed to create http request: %v", tt.name, err)
		}
		_, err = tr.RoundTrip(req)

		ws, ok := status.FromError(tt.err)
		grpclog.Infof("wstatus = %v", ws)
		// Make sure the gRPC statuses match, otherwise compare the errors.
		if ok {
			gs, ok := status.FromError(err)
			grpclog.Infof("gstatus = %v", gs)
			if !ok {
				t.Errorf("[%s] unexpected error: want status %v, got non status %v", tt.name, tt.err, err)
			}
			if !errors.Is(err, ws.Err()) {
				t.Errorf("[%s] unexpected status code: want %v, got %v", tt.name, ws, gs)
			}
		} else {
			if err != tt.err {
				t.Errorf("[%s] unexpected error: want %v, got %v", tt.name, tt.err, err)
			}
		}
		fc, ok := tt.client.(*fakeClient)
		if !ok {
			grpclog.Errorf("couldn't convert client to fakeClient %v", fc)
		}
	}
}
