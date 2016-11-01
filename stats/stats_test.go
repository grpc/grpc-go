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

package stats_test

import (
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"

	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/stats"
	testpb "google.golang.org/grpc/stats/grpc_testing"
)

func TestStartStop(t *testing.T) {
	stats.RegisterHandler(nil)
	defer stats.Stop() // Stop stats in the case of the first Fatalf.
	if stats.On() != true {
		t.Fatalf("after start.RegisterCallBack(_), stats.On() = false, want true")
	}
	stats.Stop()
	if stats.On() != false {
		t.Fatalf("after start.Stop(), stats.On() = false, want true")
	}
}

var (
	// For headers:
	testMetadata = metadata.MD{
		"key1": []string{"value1"},
		"key2": []string{"value2"},
	}
	// For trailers:
	testTrailerMetadata = metadata.MD{
		"tkey1": []string{"trailerValue1"},
		"tkey2": []string{"trailerValue2"},
	}
	// The id for which the service handler should return error.
	errorID int32 = 32202
)

type testServer struct{}

func (s *testServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	md, ok := metadata.FromContext(ctx)
	if ok {
		if err := grpc.SendHeader(ctx, md); err != nil {
			return nil, grpc.Errorf(grpc.Code(err), "grpc.SendHeader(_, %v) = %v, want <nil>", md, err)
		}
		if err := grpc.SetTrailer(ctx, testTrailerMetadata); err != nil {
			return nil, grpc.Errorf(grpc.Code(err), "grpc.SetTrailer(_, %v) = %v, want <nil>", testTrailerMetadata, err)
		}
	}

	if in.Id == errorID {
		return nil, fmt.Errorf("got error id: %v", in.Id)
	}

	return &testpb.SimpleResponse{Id: in.Id}, nil
}

func (s *testServer) FullDuplexCall(stream testpb.TestService_FullDuplexCallServer) error {
	md, ok := metadata.FromContext(stream.Context())
	if ok {
		if err := stream.SendHeader(md); err != nil {
			return grpc.Errorf(grpc.Code(err), "%v.SendHeader(%v) = %v, want %v", stream, md, err, nil)
		}
		stream.SetTrailer(testTrailerMetadata)
	}
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			return err
		}

		if in.Id == errorID {
			return fmt.Errorf("got error id: %v", in.Id)
		}

		if err := stream.Send(&testpb.SimpleResponse{Id: in.Id}); err != nil {
			return err
		}
	}
}

// test is an end-to-end test. It should be created with the newTest
// func, modified as needed, and then started with its startServer method.
// It should be cleaned up with the tearDown method.
type test struct {
	t        *testing.T
	compress string

	ctx    context.Context // valid for life of test, before tearDown
	cancel context.CancelFunc

	testServer testpb.TestServiceServer // nil means none
	// srv and srvAddr are set once startServer is called.
	srv     *grpc.Server
	srvAddr string

	cc *grpc.ClientConn // nil until requested via clientConn
}

func (te *test) tearDown() {
	if te.cancel != nil {
		te.cancel()
		te.cancel = nil
	}
	if te.cc != nil {
		te.cc.Close()
		te.cc = nil
	}
	te.srv.Stop()
}

// newTest returns a new test using the provided testing.T and
// environment.  It is returned with default values. Tests should
// modify it before calling its startServer and clientConn methods.
func newTest(t *testing.T, compress string) *test {
	te := &test{t: t, compress: compress}
	te.ctx, te.cancel = context.WithCancel(context.Background())
	return te
}

// startServer starts a gRPC server listening. Callers should defer a
// call to te.tearDown to clean up.
func (te *test) startServer(ts testpb.TestServiceServer) {
	te.testServer = ts
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		te.t.Fatalf("Failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	if te.compress == "gzip" {
		opts = append(opts,
			grpc.RPCCompressor(grpc.NewGZIPCompressor()),
			grpc.RPCDecompressor(grpc.NewGZIPDecompressor()),
		)
	}
	s := grpc.NewServer(opts...)
	te.srv = s
	if te.testServer != nil {
		testpb.RegisterTestServiceServer(s, te.testServer)
	}
	_, port, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		te.t.Fatalf("Failed to parse listener address: %v", err)
	}
	addr := "127.0.0.1:" + port

	go s.Serve(lis)
	te.srvAddr = addr
}

func (te *test) clientConn() *grpc.ClientConn {
	if te.cc != nil {
		return te.cc
	}
	opts := []grpc.DialOption{grpc.WithInsecure()}
	if te.compress == "gzip" {
		opts = append(opts,
			grpc.WithCompressor(grpc.NewGZIPCompressor()),
			grpc.WithDecompressor(grpc.NewGZIPDecompressor()),
		)
	}

	var err error
	te.cc, err = grpc.Dial(te.srvAddr, opts...)
	if err != nil {
		te.t.Fatalf("Dial(%q) = %v", te.srvAddr, err)
	}
	return te.cc
}

func (te *test) doUnaryCall(success bool) (*testpb.SimpleRequest, *testpb.SimpleResponse, error) {
	var (
		resp *testpb.SimpleResponse
		req  *testpb.SimpleRequest
		err  error
	)
	tc := testpb.NewTestServiceClient(te.clientConn())
	if success {
		req = &testpb.SimpleRequest{Id: 1}
	} else {
		req = &testpb.SimpleRequest{Id: errorID}
	}
	ctx := metadata.NewContext(context.Background(), testMetadata)

	resp, err = tc.UnaryCall(ctx, req, grpc.FailFast(false))
	if err != nil {
		return req, resp, err
	}

	return req, resp, err
}

func (te *test) doFullDuplexCallRoundtrip(count int, success bool) ([]*testpb.SimpleRequest, []*testpb.SimpleResponse, error) {
	var (
		reqs  []*testpb.SimpleRequest
		resps []*testpb.SimpleResponse
		err   error
	)
	tc := testpb.NewTestServiceClient(te.clientConn())
	stream, err := tc.FullDuplexCall(metadata.NewContext(context.Background(), testMetadata))
	if err != nil {
		return reqs, resps, err
	}
	var startID int32
	if !success {
		startID = errorID
	}
	for i := 0; i < count; i++ {
		req := &testpb.SimpleRequest{
			Id: int32(i) + startID,
		}
		reqs = append(reqs, req)
		if err = stream.Send(req); err != nil {
			return reqs, resps, err
		}
		var resp *testpb.SimpleResponse
		if resp, err = stream.Recv(); err != nil {
			return reqs, resps, err
		}
		resps = append(resps, resp)
	}
	if err = stream.CloseSend(); err != nil {
		return reqs, resps, err
	}
	if _, err = stream.Recv(); err != io.EOF {
		return reqs, resps, err
	}

	return reqs, resps, err
}

type expectedData struct {
	method         string
	localAddr      string
	encryption     string
	expectedInIdx  int
	incoming       []*testpb.SimpleRequest
	expectedOutIdx int
	outgoing       []*testpb.SimpleResponse
	err            error
}

type gotData struct {
	ctx context.Context
	s   stats.Stats
}

func checkInitStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.InitStats
	)
	if st, ok = d.s.(*stats.InitStats); !ok {
		t.Fatalf("got %T, want InitStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st IsClient = true, want false")
	}
	if st.Method != e.method {
		t.Fatalf("st.Method = %s, want %v", st.Method, e.method)
	}
	if st.LocalAddr.String() != e.localAddr {
		t.Fatalf("st.LocalAddr = %v, want %v", st.LocalAddr, e.localAddr)
	}
	if st.Encryption != e.encryption {
		t.Fatalf("st.Encryption = %v, want %v", st.Encryption, e.encryption)
	}
}

func checkIncomingHeaderStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.IncomingHeaderStats
	)
	if st, ok = d.s.(*stats.IncomingHeaderStats); !ok {
		t.Fatalf("got %T, want IncomingHeaderStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st.IsClient = true, want false")
	}
	// TODO check real length, not just > 0.
	if st.WireLength <= 0 {
		t.Fatalf("st.Lenght = 0, want > 0")
	}
}

func checkIncomingPayloadStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.IncomingPayloadStats
	)
	if st, ok = d.s.(*stats.IncomingPayloadStats); !ok {
		t.Fatalf("got %T, want IncomingPayloadStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st IsClient = true, want false")
	}
	b, err := proto.Marshal(e.incoming[e.expectedInIdx])
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}
	if reflect.TypeOf(st.Payload) != reflect.TypeOf(e.incoming[e.expectedInIdx]) {
		t.Fatalf("st.Payload = %T, want %T", st.Payload, e.incoming[e.expectedInIdx])
	}
	e.expectedInIdx++
	if string(st.Data) != string(b) {
		t.Fatalf("st.Data = %v, want %v", st.Data, b)
	}
	if st.Length != len(b) {
		t.Fatalf("st.Lenght = %v, want %v", st.Length, len(b))
	}
	// TODO check WireLength and ReceivedTime.
	if st.ReceivedTime.IsZero() {
		t.Fatalf("st.ReceivedTime = %v, want <non-zero>", st.ReceivedTime)
	}
}

func checkIncomingTrailerStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.IncomingTrailerStats
	)
	if st, ok = d.s.(*stats.IncomingTrailerStats); !ok {
		t.Fatalf("got %T, want IncomingTrailerStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st.IsClient = true, want false")
	}
	// TODO check real length, not just > 0.
	if st.WireLength <= 0 {
		t.Fatalf("st.Lenght = 0, want > 0")
	}
}

func checkOutgoingHeaderStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.OutgoingHeaderStats
	)
	if st, ok = d.s.(*stats.OutgoingHeaderStats); !ok {
		t.Fatalf("got %T, want OutgoingHeaderStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st IsClient = true, want false")
	}
	// TODO check real length, not just > 0.
	if st.WireLength <= 0 {
		t.Fatalf("st.Lenght = 0, want > 0")
	}
}

func checkOutgoingPayloadStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.OutgoingPayloadStats
	)
	if st, ok = d.s.(*stats.OutgoingPayloadStats); !ok {
		t.Fatalf("got %T, want OutgoingPayloadStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st IsClient = true, want false")
	}
	b, err := proto.Marshal(e.outgoing[e.expectedOutIdx])
	if err != nil {
		t.Fatalf("failed to marshal message: %v", err)
	}
	if reflect.TypeOf(st.Payload) != reflect.TypeOf(e.outgoing[e.expectedOutIdx]) {
		t.Fatalf("st.Payload = %T, want %T", st.Payload, e.outgoing[e.expectedOutIdx])
	}
	e.expectedOutIdx++
	if string(st.Data) != string(b) {
		t.Fatalf("st.Data = %v, want %v", st.Data, b)
	}
	if st.Length != len(b) {
		t.Fatalf("st.Lenght = %v, want %v", st.Length, len(b))
	}
	// TODO check WireLength and ReceivedTime.
	if st.SentTime.IsZero() {
		t.Fatalf("st.SentTime = %v, want <non-zero>", st.SentTime)
	}
}

func checkOutgoingTrailerStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.OutgoingTrailerStats
	)
	if st, ok = d.s.(*stats.OutgoingTrailerStats); !ok {
		t.Fatalf("got %T, want OutgoingTrailerStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if st.IsClient {
		t.Fatalf("st IsClient = true, want false")
	}
	// TODO check real length, not just > 0.
	if st.WireLength <= 0 {
		t.Fatalf("st.Lenght = 0, want > 0")
	}
}

func checkErrorStats(t *testing.T, d *gotData, e *expectedData) {
	var (
		ok bool
		st *stats.ErrorStats
	)
	if st, ok = d.s.(*stats.ErrorStats); !ok {
		t.Fatalf("got %T, want ErrorStats", d.s)
	}
	if d.ctx == nil {
		t.Fatalf("d.ctx = nil, want <non-nil>")
	}
	if grpc.Code(st.Error) != grpc.Code(e.err) || grpc.ErrorDesc(st.Error) != grpc.ErrorDesc(e.err) {
		t.Fatalf("st.Error = %v, want %v", st.Error, e.err)
	}
}

func TestServerStatsUnaryRPC(t *testing.T) {
	var (
		mu  sync.Mutex
		got []*gotData
	)
	stats.RegisterHandler(func(ctx context.Context, s stats.Stats) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, &gotData{ctx, s})
	})

	te := newTest(t, "")
	te.startServer(&testServer{})
	defer te.tearDown()

	req, resp, err := te.doUnaryCall(true)
	if err != nil {
		t.Fatalf(err.Error())
	}
	te.srv.GracefulStop() // Wait for the server to stop.

	expect := &expectedData{
		method:    "/grpc.testing.TestService/UnaryCall",
		localAddr: te.srvAddr,
		incoming:  []*testpb.SimpleRequest{req},
		outgoing:  []*testpb.SimpleResponse{resp},
	}

	checkFuncs := []func(t *testing.T, d *gotData, e *expectedData){
		checkInitStats,
		checkIncomingHeaderStats,
		checkIncomingPayloadStats,
		checkOutgoingHeaderStats,
		checkOutgoingPayloadStats,
		checkOutgoingTrailerStats,
	}

	if len(got) != len(checkFuncs) {
		t.Fatalf("got %v stats, want %v stats", len(got), len(checkFuncs))
	}

	for i, f := range checkFuncs {
		mu.Lock()
		f(t, got[i], expect)
		mu.Unlock()
	}

	stats.Stop()
}

func TestServerStatsUnaryRPCError(t *testing.T) {
	var (
		mu  sync.Mutex
		got []*gotData
	)
	stats.RegisterHandler(func(ctx context.Context, s stats.Stats) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, &gotData{ctx, s})
	})

	te := newTest(t, "")
	te.startServer(&testServer{})
	defer te.tearDown()

	req, resp, err := te.doUnaryCall(false)
	if err == nil {
		t.Fatalf("got error <nil>; want <non-nil>")
	}
	te.srv.GracefulStop() // Wait for the server to stop.

	expect := &expectedData{
		method:    "/grpc.testing.TestService/UnaryCall",
		localAddr: te.srvAddr,
		incoming:  []*testpb.SimpleRequest{req},
		outgoing:  []*testpb.SimpleResponse{resp},
		err:       err,
	}

	checkFuncs := []func(t *testing.T, d *gotData, e *expectedData){
		checkInitStats,
		checkIncomingHeaderStats,
		checkIncomingPayloadStats,
		checkOutgoingHeaderStats,
		checkOutgoingTrailerStats,
		checkErrorStats,
	}

	if len(got) != len(checkFuncs) {
		t.Fatalf("got %v stats, want %v stats", len(got), len(checkFuncs))
	}

	for i, f := range checkFuncs {
		mu.Lock()
		f(t, got[i], expect)
		mu.Unlock()
	}

	stats.Stop()
}

func TestServerStatsStreamingRPC(t *testing.T) {
	var (
		mu  sync.Mutex
		got []*gotData
	)
	stats.RegisterHandler(func(ctx context.Context, s stats.Stats) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, &gotData{ctx, s})
	})

	te := newTest(t, "gzip")
	te.startServer(&testServer{})
	defer te.tearDown()

	count := 5
	reqs, resps, err := te.doFullDuplexCallRoundtrip(count, true)
	if err == nil {
		t.Fatalf(err.Error())
	}
	te.srv.GracefulStop() // Wait for the server to stop.

	expect := &expectedData{
		method:     "/grpc.testing.TestService/FullDuplexCall",
		localAddr:  te.srvAddr,
		encryption: "gzip",
		incoming:   reqs,
		outgoing:   resps,
	}

	checkFuncs := []func(t *testing.T, d *gotData, e *expectedData){
		checkInitStats,
		checkIncomingHeaderStats,
		checkOutgoingHeaderStats,
	}
	ioPayFuncs := []func(t *testing.T, d *gotData, e *expectedData){
		checkIncomingPayloadStats,
		checkOutgoingPayloadStats,
	}
	for i := 0; i < count; i++ {
		checkFuncs = append(checkFuncs, ioPayFuncs...)
	}
	checkFuncs = append(checkFuncs, checkOutgoingTrailerStats)

	if len(got) != len(checkFuncs) {
		t.Fatalf("got %v stats, want %v stats", len(got), len(checkFuncs))
	}

	for i, f := range checkFuncs {
		mu.Lock()
		f(t, got[i], expect)
		mu.Unlock()
	}

	stats.Stop()
}

func TestServerStatsStreamingRPCError(t *testing.T) {
	var (
		mu  sync.Mutex
		got []*gotData
	)
	stats.RegisterHandler(func(ctx context.Context, s stats.Stats) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, &gotData{ctx, s})
	})

	te := newTest(t, "gzip")
	te.startServer(&testServer{})
	defer te.tearDown()

	count := 5
	reqs, resps, err := te.doFullDuplexCallRoundtrip(count, false)
	if err == nil {
		t.Fatalf("got error <nil>; want <non-nil>")
	}
	te.srv.GracefulStop() // Wait for the server to stop.

	expect := &expectedData{
		method:     "/grpc.testing.TestService/FullDuplexCall",
		localAddr:  te.srvAddr,
		encryption: "gzip",
		incoming:   reqs,
		outgoing:   resps,
		err:        err,
	}

	checkFuncs := []func(t *testing.T, d *gotData, e *expectedData){
		checkInitStats,
		checkIncomingHeaderStats,
		checkOutgoingHeaderStats,
		checkIncomingPayloadStats,
		checkOutgoingTrailerStats,
		checkErrorStats,
	}

	if len(got) != len(checkFuncs) {
		t.Fatalf("got %v stats, want %v stats", len(got), len(checkFuncs))
	}

	for i, f := range checkFuncs {
		mu.Lock()
		f(t, got[i], expect)
		mu.Unlock()
	}

	stats.Stop()
}
