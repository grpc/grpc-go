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

package main

import (
	"runtime"
	"strconv"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/benchmark"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"
)

var (
	// File path related to google.golang.org/grpc.
	certFile = "benchmark/server/testdata/server1.pem"
	keyFile  = "benchmark/server/testdata/server1.key"
)

type benchmarkServer struct {
	port          int
	cores         int
	close         func()
	mu            sync.RWMutex
	lastResetTime time.Time
}

func startBenchmarkServerWithSetup(setup *testpb.ServerConfig, serverPort int) (*benchmarkServer, error) {
	var opts []grpc.ServerOption

	// Some setup options are ignored:
	// - server type:
	//     will always start sync server
	// - async server threads
	// - core list
	grpclog.Printf(" * server type: %v (ignored, always starts sync server)", setup.ServerType)
	switch setup.ServerType {
	case testpb.ServerType_SYNC_SERVER:
	case testpb.ServerType_ASYNC_SERVER:
	case testpb.ServerType_ASYNC_GENERIC_SERVER:
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknow server type: %v", setup.ServerType)
	}
	grpclog.Printf(" * async server threads: %v (ignored)", setup.AsyncServerThreads)
	grpclog.Printf(" * core list: %v (ignored)", setup.CoreList)

	grpclog.Printf(" - security params: %v", setup.SecurityParams)
	if setup.SecurityParams != nil {
		creds, err := credentials.NewServerTLSFromFile(Abs(certFile), Abs(keyFile))
		if err != nil {
			grpclog.Fatalf("failed to generate credentials %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	grpclog.Printf(" - core limit: %v", setup.CoreLimit)
	// Use one cpu core by default.
	numOfCores := 1
	if setup.CoreLimit > 0 {
		numOfCores = int(setup.CoreLimit)
	}
	runtime.GOMAXPROCS(numOfCores)

	grpclog.Printf(" - port: %v", setup.Port)
	var port int
	// Priority: setup.Port > serverPort > default (0).
	if setup.Port != 0 {
		port = int(setup.Port)
	} else if serverPort != 0 {
		port = serverPort
	}

	grpclog.Printf(" - payload config: %v", setup.PayloadConfig)
	var p int
	var close func()
	if setup.PayloadConfig != nil {
		switch payload := setup.PayloadConfig.Payload.(type) {
		case *testpb.PayloadConfig_BytebufParams:
			opts = append(opts, grpc.CustomCodec(byteBufCodec{}))
			p, close = benchmark.StartGenericServer(":"+strconv.Itoa(port), payload.BytebufParams.ReqSize, payload.BytebufParams.RespSize, opts...)
		case *testpb.PayloadConfig_SimpleParams:
			p, close = benchmark.StartServer(":"+strconv.Itoa(port), opts...)
		case *testpb.PayloadConfig_ComplexParams:
			return nil, grpc.Errorf(codes.Unimplemented, "unsupported payload config: %v", setup.PayloadConfig)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", setup.PayloadConfig)
		}
	} else {
		// Start protobuf server is payload config is nil.
		p, close = benchmark.StartServer(":"+strconv.Itoa(port), opts...)
	}

	grpclog.Printf("benchmark server listening at port %v", p)

	return &benchmarkServer{port: p, cores: numOfCores, close: close, lastResetTime: time.Now()}, nil
}

func (bs *benchmarkServer) getStats() *testpb.ServerStats {
	// TODO wall time, sys time, user time.
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return &testpb.ServerStats{TimeElapsed: time.Since(bs.lastResetTime).Seconds(), TimeUser: 0, TimeSystem: 0}
}

func (bs *benchmarkServer) reset() {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.lastResetTime = time.Now()
}
