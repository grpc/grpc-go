/*
 *
 * Copyright 2014 gRPC authors.
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

/*
Package benchmark implements the building blocks to setup end-to-end gRPC benchmarks.
*/
package benchmark

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/benchmark/grpc_testing"
	"google.golang.org/grpc/benchmark/latency"
	"google.golang.org/grpc/benchmark/stats"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
)

// Features contains most fields for a benchmark
type Features struct {
	EnableTrace        bool
	Md                 metadata.MD
	Latency            time.Duration
	Kbps               int
	Mtu                int
	MaxConcurrentCalls int
	MaxConnCount       int
	ReqSizeBytes       int
	RespSizeBytes      int
}

func (f Features) String() string {
	return fmt.Sprintf("latency_%s-kbps_%#v-MTU_%#v-maxConcurrentCalls_"+
		"%#v-maxConn_%#v-reqSize_%#vB-respSize_%#vB",
		f.Latency.String(), f.Kbps, f.Mtu, f.MaxConcurrentCalls, f.MaxConnCount, f.ReqSizeBytes, f.RespSizeBytes)
}

// AddOne add 1 to the features slice
func AddOne(features []int, upperBound []int) {
	for i := len(features) - 1; i >= 0; i-- {
		features[i] = (features[i] + 1)
		if features[i]/upperBound[i] == 0 {
			break
		}
		features[i] = features[i] % upperBound[i]
	}
}

// Allows reuse of the same testpb.Payload object.
func setPayload(p *testpb.Payload, t testpb.PayloadType, size int) {
	if size < 0 {
		grpclog.Fatalf("Requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	case testpb.PayloadType_UNCOMPRESSABLE:
		grpclog.Fatalf("PayloadType UNCOMPRESSABLE is not supported")
	default:
		grpclog.Fatalf("Unsupported payload type: %d", t)
	}
	p.Type = t
	p.Body = body
	return
}

func newPayload(t testpb.PayloadType, size int) *testpb.Payload {
	p := new(testpb.Payload)
	setPayload(p, t, size)
	return p
}

type testServer struct {
}

func (s *testServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{
		Payload: newPayload(in.ResponseType, int(in.ResponseSize)),
	}, nil
}

func (s *testServer) StreamingCall(stream testpb.BenchmarkService_StreamingCallServer) error {
	response := &testpb.SimpleResponse{
		Payload: new(testpb.Payload),
	}
	in := new(testpb.SimpleRequest)
	for {
		// use ServerStream directly to reuse the same testpb.SimpleRequest object
		err := stream.(grpc.ServerStream).RecvMsg(in)
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			return err
		}
		setPayload(response.Payload, in.ResponseType, int(in.ResponseSize))
		if err := stream.Send(response); err != nil {
			return err
		}
	}
}

// byteBufServer is a gRPC server that sends and receives byte buffer.
// The purpose is to benchmark the gRPC performance without protobuf serialization/deserialization overhead.
type byteBufServer struct {
	respSize int32
}

// UnaryCall is an empty function and is not used for benchmark.
// If bytebuf UnaryCall benchmark is needed later, the function body needs to be updated.
func (s *byteBufServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	metadata.FromContext(ctx)
	return &testpb.SimpleResponse{}, nil
}

func (s *byteBufServer) StreamingCall(stream testpb.BenchmarkService_StreamingCallServer) error {
	metadata.FromContext(stream.Context())
	for {
		var in []byte
		err := stream.(grpc.ServerStream).RecvMsg(&in)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		out := make([]byte, s.respSize)
		if err := stream.(grpc.ServerStream).SendMsg(&out); err != nil {
			return err
		}
	}
}

// ServerInfo contains the information to create a gRPC benchmark server.
type ServerInfo struct {
	// Addr is the address of the server.
	Addr string

	// Type is the type of the server.
	// It should be "protobuf" or "bytebuf".
	Type string

	// Metadata is an optional configuration.
	// For "protobuf", it's ignored.
	// For "bytebuf", it should be an int representing response size.
	Metadata interface{}

	// Network can simulate latency
	Network *latency.Network
}

// StartServer starts a gRPC server serving a benchmark service according to info.
// It returns its listen address and a function to stop the server.
func StartServer(info ServerInfo, opts ...grpc.ServerOption) (string, func()) {
	lis, err := net.Listen("tcp", info.Addr)
	if err != nil {
		grpclog.Fatalf("Failed to listen: %v", err)
	}
	nw := info.Network
	if nw != nil {
		lis = nw.Listener(lis)
	}
	s := grpc.NewServer(opts...)
	switch info.Type {
	case "protobuf":
		testpb.RegisterBenchmarkServiceServer(s, &testServer{})
	case "bytebuf":
		respSize, ok := info.Metadata.(int32)
		if !ok {
			grpclog.Fatalf("failed to StartServer, invalid metadata: %v, for Type: %v", info.Metadata, info.Type)
		}
		testpb.RegisterBenchmarkServiceServer(s, &byteBufServer{respSize: respSize})
	default:
		grpclog.Fatalf("failed to StartServer, unknown Type: %v", info.Type)
	}
	go s.Serve(lis)
	return lis.Addr().String(), func() {
		s.Stop()
	}
}

// DoUnaryCall performs an unary RPC with given stub and request and response sizes.
func DoUnaryCall(tc testpb.BenchmarkServiceClient, md metadata.MD, reqSize, respSize int) error {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, reqSize)
	req := &testpb.SimpleRequest{
		ResponseType: pl.Type,
		ResponseSize: int32(respSize),
		Payload:      pl,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), md)
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		return fmt.Errorf("/BenchmarkService/UnaryCall(_, _) = _, %v, want _, <nil>", err)
	}
	return nil
}

// DoStreamingRoundTrip performs a round trip for a single streaming rpc.
func DoStreamingRoundTrip(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) error {
	pl := newPayload(testpb.PayloadType_COMPRESSABLE, reqSize)
	req := &testpb.SimpleRequest{
		ResponseType: pl.Type,
		ResponseSize: int32(respSize),
		Payload:      pl,
	}
	if err := stream.Send(req); err != nil {
		return fmt.Errorf("/BenchmarkService/StreamingCall.Send(_) = %v, want <nil>", err)
	}
	if _, err := stream.Recv(); err != nil {
		// EOF is a valid error here.
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("/BenchmarkService/StreamingCall.Recv(_) = %v, want <nil>", err)
	}
	return nil
}

// DoByteBufStreamingRoundTrip performs a round trip for a single streaming rpc, using a custom codec for byte buffer.
func DoByteBufStreamingRoundTrip(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) error {
	out := make([]byte, reqSize)
	if err := stream.(grpc.ClientStream).SendMsg(&out); err != nil {
		return fmt.Errorf("/BenchmarkService/StreamingCall.(ClientStream).SendMsg(_) = %v, want <nil>", err)
	}
	var in []byte
	if err := stream.(grpc.ClientStream).RecvMsg(&in); err != nil {
		// EOF is a valid error here.
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("/BenchmarkService/StreamingCall.(ClientStream).RecvMsg(_) = %v, want <nil>", err)
	}
	return nil
}

// NewClientConn creates a gRPC client connection to addr.
func NewClientConn(addr string, opts ...grpc.DialOption) *grpc.ClientConn {
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		grpclog.Fatalf("NewClientConn(%q) failed to create a ClientConn %v", addr, err)
	}
	return conn
}

// RunUnary runs unary mode.
// startTimer() and stopTimer() calculate the stats. stopBench() defines how to stop bench, like for loop or timeout.
// s passes the histogram out of the function.
func RunUnary(startTimer, stopTimer func(), stopBench func(chan int), s *stats.Stats, benchFeatures Features) {
	if s == nil {
		s = stats.AddStats(&testing.B{}, 38)
	} else {
		s.Clear()
	}
	nw := &latency.Network{Kbps: benchFeatures.Kbps, Latency: benchFeatures.Latency, MTU: benchFeatures.Mtu}
	stopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf", Network: nw}, grpc.MaxConcurrentStreams(uint32(benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls+1)))
	defer stopper()
	conns := make([]*grpc.ClientConn, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	clients := make([]testpb.BenchmarkServiceClient, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	for ic := 0; ic < benchFeatures.MaxConnCount; ic++ {
		conns[ic] = NewClientConn(
			target, grpc.WithInsecure(),
			grpc.WithDialer(func(address string, timeout time.Duration) (net.Conn, error) {
				return nw.TimeoutDialer(net.DialTimeout)("tcp", address, timeout)
			}),
		)
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		// Warm up.
		for i := 0; i < 10; i++ {
			unaryCaller(tc, benchFeatures.Md, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
		}
		clients[ic] = tc
	}

	ch := make(chan int, benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(benchFeatures.MaxConnCount * benchFeatures.MaxConcurrentCalls)

	// Distribute the b.N calls over maxConcurrentCalls workers.
	for _, tc := range clients {
		for i := 0; i < benchFeatures.MaxConcurrentCalls; i++ {
			go func() {
				for range ch {
					start := time.Now()
					unaryCaller(tc, benchFeatures.Md, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
					elapse := time.Since(start)
					mu.Lock()
					s.Add(elapse)
					mu.Unlock()
				}
				wg.Done()
			}()
		}
	}
	startTimer()
	stopBench(ch)
	stopTimer()
	close(ch)
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
}

// RunStream runs stream mode.
func RunStream(startTimer, stopTimer func(), stopBench func(chan int), s *stats.Stats, benchFeatures Features) {
	if s == nil {
		s = stats.AddStats(&testing.B{}, 38)
	} else {
		s.Clear()
	}
	nw := &latency.Network{Kbps: benchFeatures.Kbps, Latency: benchFeatures.Latency, MTU: benchFeatures.Mtu}
	stopTimer()
	target, stopper := StartServer(ServerInfo{Addr: "localhost:0", Type: "protobuf", Network: nw}, grpc.MaxConcurrentStreams(uint32(benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls+1)))
	defer stopper()
	conns := make([]*grpc.ClientConn, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	clients := make([]testpb.BenchmarkServiceClient, benchFeatures.MaxConnCount, benchFeatures.MaxConnCount)
	ctx := metadata.NewContext(context.Background(), benchFeatures.Md)
	for ic := 0; ic < benchFeatures.MaxConnCount; ic++ {
		conns[ic] = NewClientConn(
			target, grpc.WithInsecure(),
			grpc.WithDialer(func(address string, timeout time.Duration) (net.Conn, error) {
				return nw.TimeoutDialer(net.DialTimeout)("tcp", address, timeout)
			}),
		)
		// Warm up connection.
		tc := testpb.NewBenchmarkServiceClient(conns[ic])
		stream, err := tc.StreamingCall(ctx)
		if err != nil {
			grpclog.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
		}
		for i := 0; i < 10; i++ {
			streamCaller(stream, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
		}
		clients[ic] = tc
	}
	ch := make(chan int, benchFeatures.MaxConnCount*benchFeatures.MaxConcurrentCalls*4)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	wg.Add(benchFeatures.MaxConnCount * benchFeatures.MaxConcurrentCalls)
	// Distribute the b.N calls over maxConcurrentCalls workers.
	for _, tc := range clients {
		for i := 0; i < benchFeatures.MaxConcurrentCalls; i++ {
			stream, err := tc.StreamingCall(ctx)
			if err != nil {
				grpclog.Fatalf("%v.StreamingCall(_) = _, %v", tc, err)
			}
			go func() {
				for range ch {
					start := time.Now()
					streamCaller(stream, benchFeatures.ReqSizeBytes, benchFeatures.RespSizeBytes)
					elapse := time.Since(start)
					mu.Lock()
					s.Add(elapse)
					mu.Unlock()
				}
				wg.Done()
			}()
		}
	}
	startTimer()
	stopBench(ch)
	stopTimer()
	close(ch)
	wg.Wait()
	for _, conn := range conns {
		conn.Close()
	}
}

func unaryCaller(client testpb.BenchmarkServiceClient, md metadata.MD, reqSize, respSize int) {
	if err := DoUnaryCall(client, md, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoUnaryCall failed: %v", err)
	}
}

func streamCaller(stream testpb.BenchmarkService_StreamingCallClient, reqSize, respSize int) {
	if err := DoStreamingRoundTrip(stream, reqSize, respSize); err != nil {
		grpclog.Fatalf("DoStreamingRoundTrip failed: %v", err)
	}
}
