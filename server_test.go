/*
 *
 * Copyright 2016, Google Inc.
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *     * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *     * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *     * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 */

package grpc

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

type emptyServiceServer interface{}

type testServer struct{}

var (
	testSd = ServiceDesc{
		ServiceName: "grpc.testing.EmptyService",
		HandlerType: (*emptyServiceServer)(nil),
		Methods: []MethodDesc{
			{
				MethodName: "EmptyCall",
				Handler:    nil,
			},
		},
		Streams: []StreamDesc{
			{
				StreamName:    "EmptyStream",
				Handler:       nil,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
		Metadata: testFd,
	}
	testFd = []byte{0, 1, 2, 3}
)

func TestStopBeforeServe(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	server := NewServer()
	server.Stop()
	err = server.Serve(lis)
	if err != ErrServerStopped {
		t.Fatalf("server.Serve() error = %v, want %v", err, ErrServerStopped)
	}

	// server.Serve is responsible for closing the listener, even if the
	// server was already stopped.
	err = lis.Close()
	if got, want := ErrorDesc(err), "use of closed network connection"; !strings.Contains(got, want) {
		t.Errorf("Close() error = %q, want %q", got, want)
	}
}

func TestMetadata(t *testing.T) {
	server := NewServer()
	server.RegisterService(&testSd, &testServer{})

	for _, test := range []struct {
		name string
		want []byte
	}{
		{"grpc.testing.EmptyService", testFd},
		{"grpc.testing.EmptyService.EmptyCall", testFd},
		{"grpc.testing.EmptyService.EmptyStream", testFd},
	} {
		meta := server.Metadata(test.name)
		var (
			fd []byte
			ok bool
		)
		if fd, ok = meta.([]byte); !ok {
			t.Errorf("Metadata(%q)=%v, want %v", test.name, meta, test.want)
		}
		if !reflect.DeepEqual(fd, test.want) {
			t.Errorf("Metadata(%q)=%v, want %v", test.name, fd, test.want)
		}
	}
}

func TestMetadataNotFound(t *testing.T) {
	server := NewServer()
	server.RegisterService(&testSd, &testServer{})

	for _, test := range []string{
		"EmptyCall",
		"grpc.EmptyService",
		"grpc.EmptyService.EmptyCall",
		"grpc.testing.EmptyService.EmptyCallWrong",
		"grpc.testing.EmptyService.EmptyStreamWrong",
	} {
		meta := server.Metadata(test)
		if meta != nil {
			t.Errorf("Metadata(%q)=%v, want <nil>", test, meta)
		}
	}
}

func TestAllServiceNames(t *testing.T) {
	server := NewServer()
	server.RegisterService(&testSd, &testServer{})
	server.RegisterService(&ServiceDesc{
		ServiceName: "another.EmptyService",
		HandlerType: (*emptyServiceServer)(nil),
	}, &testServer{})
	services := server.AllServiceNames()
	want := []string{"grpc.testing.EmptyService", "another.EmptyService"}
	// Compare string slices.
	m := make(map[string]int)
	for _, s := range services {
		m[s]++
	}
	for _, s := range want {
		if m[s] > 0 {
			m[s]--
			continue
		}
		t.Fatalf("AllServiceNames() = %q, want: %q", services, want)
	}
}
