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

package http_over_grpc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/testdata"

	pb "google.golang.org/grpc/http_over_grpc/http_over_grpc_proto"
	"google.golang.org/grpc/status"
)

const (
	certFile       = "server1.pem"
	keyFile        = "server1.key"
	caCertFilePath = "ca.pem"
	eofErrorString = "readerror: EOF"
	port           = 8080
)

type testServer struct {
	Port int
}

func (s *testServer) start() {
	s.Port = port
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readerror":
			// Hijack and close the connection to simulate an EOF.
			hj, ok := w.(http.Hijacker)
			if !ok {
				grpclog.Fatalf("Couldn't hijack the response writer.")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				grpclog.Fatalf("Cloudn't hijack the connection.")
				return
			}

			if err := conn.Close(); err != nil {
				grpclog.Fatalf("Couldn't close the connection.")
			}
			w.WriteHeader(http.StatusOK)
		}
	})
	go http.ListenAndServeTLS(fmt.Sprintf(":%d", s.Port), testdata.Path(certFile), testdata.Path(keyFile), nil)
}

func TestServer(t *testing.T) {
	ctx := context.Background()

	http.DefaultServeMux = http.NewServeMux()
	s := &testServer{}
	s.start()

	baseURL := fmt.Sprintf("https://localhost:%d", s.Port)

	tlsConfig := &TLSConfig{
		CAFile:     testdata.Path(caCertFilePath),
		ServerName: "x.test.youtube.com",
	}

	ts, err := NewHTTPOverGRPCServer(tlsConfig)
	if err != nil {
		t.Fatalf("Error Connect server: %v", err)
	}

	// Wait for the server to be ready
	if err := func() error {
		var err error
		for i := 0; i < 10; i++ {
			if _, err = ts.HTTPRequest(ctx, &pb.HTTPOverGRPCRequest{
				Method:  "GET",
				Url:     baseURL + "/",
				Headers: nil,
				Body:    nil,
			}); err != nil {
				grpclog.Warningf("error checking server: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return nil
		}
		return fmt.Errorf("timeout waiting for server to be ready: %v", err)
	}(); err != nil {
		grpclog.Fatal(err)
	}

	// All tests use the same test cases.
	testCases := []struct {
		name   string
		req    *pb.HTTPOverGRPCRequest
		status *status.Status
	}{
		{
			"success",
			&pb.HTTPOverGRPCRequest{
				Method:  "GET",
				Url:     baseURL + "/",
				Headers: nil,
				Body:    nil,
			},
			nil,
		},
		{
			"read response error",
			&pb.HTTPOverGRPCRequest{
				Method:  "GET",
				Url:     baseURL + "/readerror",
				Headers: nil,
				Body:    nil,
			},
			nil,
		},
	}
	for _, tt := range testCases {
		_, err := ts.HTTPRequest(ctx, tt.req)

		if err != nil && !strings.Contains(err.Error(), eofErrorString) {
			t.Errorf("[%s]: err = %v", tt.name, err)
		}
	}
}
