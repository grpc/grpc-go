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
	// TODO change filepath
	certFile = "/usr/local/google/home/menghanl/go/src/google.golang.org/grpc/benchmark/server/testdata/server1.pem"
	keyFile  = "/usr/local/google/home/menghanl/go/src/google.golang.org/grpc/benchmark/server/testdata/server1.key"
)

type benchmarkServer struct {
	port          int
	close         func()
	mu            sync.RWMutex
	lastResetTime time.Time
}

func startBenchmarkServerWithSetup(setup *testpb.ServerConfig, serverPort int) (*benchmarkServer, error) {
	var opts []grpc.ServerOption

	grpclog.Printf(" - server type: %v", setup.ServerType)
	switch setup.ServerType {
	// Ignore server type.
	case testpb.ServerType_SYNC_SERVER:
	case testpb.ServerType_ASYNC_SERVER:
	case testpb.ServerType_ASYNC_GENERIC_SERVER:
	default:
		return nil, grpc.Errorf(codes.InvalidArgument, "unknow server type: %v", setup.ServerType)
	}

	grpclog.Printf(" - security params: %v", setup.SecurityParams)
	if setup.SecurityParams != nil {
		creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
		if err != nil {
			grpclog.Fatalf("failed to generate credentials %v", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	// Ignore async server threads.

	grpclog.Printf(" - core limit: %v", setup.CoreLimit)
	if setup.CoreLimit > 0 {
		runtime.GOMAXPROCS(int(setup.CoreLimit))
	} else {
		runtime.GOMAXPROCS(1)
	}

	grpclog.Printf(" - core list: %v", setup.CoreList)
	if len(setup.CoreList) > 0 {
		return nil, grpc.Errorf(codes.InvalidArgument, "specifying core list is not supported")
	}

	grpclog.Printf(" - port: %v", setup.Port)
	var port int
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
			return nil, grpc.Errorf(codes.InvalidArgument, "unsupported payload config: %v", setup.PayloadConfig)
		default:
			return nil, grpc.Errorf(codes.InvalidArgument, "unknow payload config: %v", setup.PayloadConfig)
		}
	} else {
		// Start protobuf server is payload config is nil
		p, close = benchmark.StartServer(":"+strconv.Itoa(port), opts...)
	}

	grpclog.Printf("benchmark server listening at port %v", p)

	bs := &benchmarkServer{port: p, close: close, lastResetTime: time.Now()}
	return bs, nil
}

func (bs *benchmarkServer) getStats() *testpb.ServerStats {
	bs.mu.RLock()
	defer bs.mu.RUnlock()
	return &testpb.ServerStats{TimeElapsed: time.Since(bs.lastResetTime).Seconds(), TimeUser: 0, TimeSystem: 0}
}

func (bs *benchmarkServer) reset() {
	// TODO wall time, sys time, user time
	bs.mu.Lock()
	defer bs.mu.Unlock()
	bs.lastResetTime = time.Now()
}
