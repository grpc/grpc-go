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

package test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	anypb "github.com/golang/protobuf/ptypes/any"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/internal"
	"google.golang.org/grpc/internal/channelz"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/stubserver"
	"google.golang.org/grpc/internal/testutils"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
	"google.golang.org/grpc/test/bufconn"
	testpb "google.golang.org/grpc/test/grpc_testing"
	"google.golang.org/grpc/testdata"
)

const defaultHealthService = "grpc.health.v1.Health"

func init() {
	channelz.TurnOn()
}

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

var (
	// For headers:
	testMetadata = metadata.MD{
		"key1":     []string{"value1"},
		"key2":     []string{"value2"},
		"key3-bin": []string{"binvalue1", string([]byte{1, 2, 3})},
	}
	testMetadata2 = metadata.MD{
		"key1": []string{"value12"},
		"key2": []string{"value22"},
	}
	// For trailers:
	testTrailerMetadata = metadata.MD{
		"tkey1":     []string{"trailerValue1"},
		"tkey2":     []string{"trailerValue2"},
		"tkey3-bin": []string{"trailerbinvalue1", string([]byte{3, 2, 1})},
	}
	testTrailerMetadata2 = metadata.MD{
		"tkey1": []string{"trailerValue12"},
		"tkey2": []string{"trailerValue22"},
	}
	// capital "Key" is illegal in HTTP/2.
	malformedHTTP2Metadata = metadata.MD{
		"Key": []string{"foo"},
	}
	testAppUA     = "myApp1/1.0 myApp2/0.9"
	failAppUA     = "fail-this-RPC"
	detailedError = status.ErrorProto(&spb.Status{
		Code:    int32(codes.DataLoss),
		Message: "error for testing: " + failAppUA,
		Details: []*anypb.Any{{
			TypeUrl: "url",
			Value:   []byte{6, 0, 0, 6, 1, 3},
		}},
	})
)

var raceMode bool // set by race.go in race mode

type testServer struct {
	testpb.UnimplementedTestServiceServer

	security           string // indicate the authentication protocol used by this server.
	earlyFail          bool   // whether to error out the execution of a service handler prematurely.
	setAndSendHeader   bool   // whether to call setHeader and sendHeader.
	setHeaderOnly      bool   // whether to only call setHeader, not sendHeader.
	multipleSetTrailer bool   // whether to call setTrailer multiple times.
	unaryCallSleepTime time.Duration
}

func (s *testServer) EmptyCall(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		// For testing purpose, returns an error if user-agent is failAppUA.
		// To test that client gets the correct error.
		if ua, ok := md["user-agent"]; !ok || strings.HasPrefix(ua[0], failAppUA) {
			return nil, detailedError
		}
		var str []string
		for _, entry := range md["user-agent"] {
			str = append(str, "ua", entry)
		}
		grpc.SendHeader(ctx, metadata.Pairs(str...))
	}
	return new(testpb.Empty), nil
}

func newPayload(t testpb.PayloadType, size int32) (*testpb.Payload, error) {
	if size < 0 {
		return nil, fmt.Errorf("requested a response with invalid length %d", size)
	}
	body := make([]byte, size)
	switch t {
	case testpb.PayloadType_COMPRESSABLE:
	case testpb.PayloadType_UNCOMPRESSABLE:
		return nil, fmt.Errorf("PayloadType UNCOMPRESSABLE is not supported")
	default:
		return nil, fmt.Errorf("unsupported payload type: %d", t)
	}
	return &testpb.Payload{
		Type: t,
		Body: body,
	}, nil
}

func (s *testServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if _, exists := md[":authority"]; !exists {
			return nil, status.Errorf(codes.DataLoss, "expected an :authority metadata: %v", md)
		}
		if s.setAndSendHeader {
			if err := grpc.SetHeader(ctx, md); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SetHeader(_, %v) = %v, want <nil>", md, err)
			}
			if err := grpc.SendHeader(ctx, testMetadata2); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SendHeader(_, %v) = %v, want <nil>", testMetadata2, err)
			}
		} else if s.setHeaderOnly {
			if err := grpc.SetHeader(ctx, md); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SetHeader(_, %v) = %v, want <nil>", md, err)
			}
			if err := grpc.SetHeader(ctx, testMetadata2); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SetHeader(_, %v) = %v, want <nil>", testMetadata2, err)
			}
		} else {
			if err := grpc.SendHeader(ctx, md); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SendHeader(_, %v) = %v, want <nil>", md, err)
			}
		}
		if err := grpc.SetTrailer(ctx, testTrailerMetadata); err != nil {
			return nil, status.Errorf(status.Code(err), "grpc.SetTrailer(_, %v) = %v, want <nil>", testTrailerMetadata, err)
		}
		if s.multipleSetTrailer {
			if err := grpc.SetTrailer(ctx, testTrailerMetadata2); err != nil {
				return nil, status.Errorf(status.Code(err), "grpc.SetTrailer(_, %v) = %v, want <nil>", testTrailerMetadata2, err)
			}
		}
	}
	pr, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.DataLoss, "failed to get peer from ctx")
	}
	if pr.Addr == net.Addr(nil) {
		return nil, status.Error(codes.DataLoss, "failed to get peer address")
	}
	if s.security != "" {
		// Check Auth info
		var authType, serverName string
		switch info := pr.AuthInfo.(type) {
		case credentials.TLSInfo:
			authType = info.AuthType()
			serverName = info.State.ServerName
		default:
			return nil, status.Error(codes.Unauthenticated, "Unknown AuthInfo type")
		}
		if authType != s.security {
			return nil, status.Errorf(codes.Unauthenticated, "Wrong auth type: got %q, want %q", authType, s.security)
		}
		if serverName != "x.test.example.com" {
			return nil, status.Errorf(codes.Unauthenticated, "Unknown server name %q", serverName)
		}
	}
	// Simulate some service delay.
	time.Sleep(s.unaryCallSleepTime)

	payload, err := newPayload(in.GetResponseType(), in.GetResponseSize())
	if err != nil {
		return nil, err
	}

	return &testpb.SimpleResponse{
		Payload: payload,
	}, nil
}

func (s *testServer) StreamingOutputCall(args *testpb.StreamingOutputCallRequest, stream testpb.TestService_StreamingOutputCallServer) error {
	if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
		if _, exists := md[":authority"]; !exists {
			return status.Errorf(codes.DataLoss, "expected an :authority metadata: %v", md)
		}
		// For testing purpose, returns an error if user-agent is failAppUA.
		// To test that client gets the correct error.
		if ua, ok := md["user-agent"]; !ok || strings.HasPrefix(ua[0], failAppUA) {
			return status.Error(codes.DataLoss, "error for testing: "+failAppUA)
		}
	}
	cs := args.GetResponseParameters()
	for _, c := range cs {
		if us := c.GetIntervalUs(); us > 0 {
			time.Sleep(time.Duration(us) * time.Microsecond)
		}

		payload, err := newPayload(args.GetResponseType(), c.GetSize())
		if err != nil {
			return err
		}

		if err := stream.Send(&testpb.StreamingOutputCallResponse{
			Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *testServer) StreamingInputCall(stream testpb.TestService_StreamingInputCallServer) error {
	var sum int
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&testpb.StreamingInputCallResponse{
				AggregatedPayloadSize: int32(sum),
			})
		}
		if err != nil {
			return err
		}
		p := in.GetPayload().GetBody()
		sum += len(p)
		if s.earlyFail {
			return status.Error(codes.NotFound, "not found")
		}
	}
}

func (s *testServer) FullDuplexCall(stream testpb.TestService_FullDuplexCallServer) error {
	md, ok := metadata.FromIncomingContext(stream.Context())
	if ok {
		if s.setAndSendHeader {
			if err := stream.SetHeader(md); err != nil {
				return status.Errorf(status.Code(err), "%v.SetHeader(_, %v) = %v, want <nil>", stream, md, err)
			}
			if err := stream.SendHeader(testMetadata2); err != nil {
				return status.Errorf(status.Code(err), "%v.SendHeader(_, %v) = %v, want <nil>", stream, testMetadata2, err)
			}
		} else if s.setHeaderOnly {
			if err := stream.SetHeader(md); err != nil {
				return status.Errorf(status.Code(err), "%v.SetHeader(_, %v) = %v, want <nil>", stream, md, err)
			}
			if err := stream.SetHeader(testMetadata2); err != nil {
				return status.Errorf(status.Code(err), "%v.SetHeader(_, %v) = %v, want <nil>", stream, testMetadata2, err)
			}
		} else {
			if err := stream.SendHeader(md); err != nil {
				return status.Errorf(status.Code(err), "%v.SendHeader(%v) = %v, want %v", stream, md, err, nil)
			}
		}
		stream.SetTrailer(testTrailerMetadata)
		if s.multipleSetTrailer {
			stream.SetTrailer(testTrailerMetadata2)
		}
	}
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			return nil
		}
		if err != nil {
			// to facilitate testSvrWriteStatusEarlyWrite
			if status.Code(err) == codes.ResourceExhausted {
				return status.Errorf(codes.Internal, "fake error for test testSvrWriteStatusEarlyWrite. true error: %s", err.Error())
			}
			return err
		}
		cs := in.GetResponseParameters()
		for _, c := range cs {
			if us := c.GetIntervalUs(); us > 0 {
				time.Sleep(time.Duration(us) * time.Microsecond)
			}

			payload, err := newPayload(in.GetResponseType(), c.GetSize())
			if err != nil {
				return err
			}

			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: payload,
			}); err != nil {
				// to facilitate testSvrWriteStatusEarlyWrite
				if status.Code(err) == codes.ResourceExhausted {
					return status.Errorf(codes.Internal, "fake error for test testSvrWriteStatusEarlyWrite. true error: %s", err.Error())
				}
				return err
			}
		}
	}
}

func (s *testServer) HalfDuplexCall(stream testpb.TestService_HalfDuplexCallServer) error {
	var msgBuf []*testpb.StreamingOutputCallRequest
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			// read done.
			break
		}
		if err != nil {
			return err
		}
		msgBuf = append(msgBuf, in)
	}
	for _, m := range msgBuf {
		cs := m.GetResponseParameters()
		for _, c := range cs {
			if us := c.GetIntervalUs(); us > 0 {
				time.Sleep(time.Duration(us) * time.Microsecond)
			}

			payload, err := newPayload(m.GetResponseType(), c.GetSize())
			if err != nil {
				return err
			}

			if err := stream.Send(&testpb.StreamingOutputCallResponse{
				Payload: payload,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type env struct {
	name         string
	network      string // The type of network such as tcp, unix, etc.
	security     string // The security protocol such as TLS, SSH, etc.
	httpHandler  bool   // whether to use the http.Handler ServerTransport; requires TLS
	balancer     string // One of "round_robin", "pick_first", or "".
	customDialer func(string, string, time.Duration) (net.Conn, error)
}

func (e env) runnable() bool {
	if runtime.GOOS == "windows" && e.network == "unix" {
		return false
	}
	return true
}

func (e env) dialer(addr string, timeout time.Duration) (net.Conn, error) {
	if e.customDialer != nil {
		return e.customDialer(e.network, addr, timeout)
	}
	return net.DialTimeout(e.network, addr, timeout)
}

var (
	tcpClearEnv   = env{name: "tcp-clear-v1-balancer", network: "tcp"}
	tcpTLSEnv     = env{name: "tcp-tls-v1-balancer", network: "tcp", security: "tls"}
	tcpClearRREnv = env{name: "tcp-clear", network: "tcp", balancer: "round_robin"}
	tcpTLSRREnv   = env{name: "tcp-tls", network: "tcp", security: "tls", balancer: "round_robin"}
	handlerEnv    = env{name: "handler-tls", network: "tcp", security: "tls", httpHandler: true, balancer: "round_robin"}
	noBalancerEnv = env{name: "no-balancer", network: "tcp", security: "tls"}
	allEnv        = []env{tcpClearEnv, tcpTLSEnv, tcpClearRREnv, tcpTLSRREnv, handlerEnv, noBalancerEnv}
)

var onlyEnv = flag.String("only_env", "", "If non-empty, one of 'tcp-clear', 'tcp-tls', 'unix-clear', 'unix-tls', or 'handler-tls' to only run the tests for that environment. Empty means all.")

func listTestEnv() (envs []env) {
	if *onlyEnv != "" {
		for _, e := range allEnv {
			if e.name == *onlyEnv {
				if !e.runnable() {
					panic(fmt.Sprintf("--only_env environment %q does not run on %s", *onlyEnv, runtime.GOOS))
				}
				return []env{e}
			}
		}
		panic(fmt.Sprintf("invalid --only_env value %q", *onlyEnv))
	}
	for _, e := range allEnv {
		if e.runnable() {
			envs = append(envs, e)
		}
	}
	return envs
}

// test is an end-to-end test. It should be created with the newTest
// func, modified as needed, and then started with its startServer method.
// It should be cleaned up with the tearDown method.
type test struct {
	// The following are setup in newTest().
	t      *testing.T
	e      env
	ctx    context.Context // valid for life of test, before tearDown
	cancel context.CancelFunc

	// The following knobs are for the server-side, and should be set after
	// calling newTest() and before calling startServer().

	// whether or not to expose the server's health via the default health
	// service implementation.
	enableHealthServer bool
	// In almost all cases, one should set the 'enableHealthServer' flag above to
	// expose the server's health using the default health service
	// implementation. This should only be used when a non-default health service
	// implementation is required.
	healthServer            healthgrpc.HealthServer
	maxStream               uint32
	tapHandle               tap.ServerInHandle
	maxServerMsgSize        *int
	maxServerReceiveMsgSize *int
	maxServerSendMsgSize    *int
	maxServerHeaderListSize *uint32
	// Used to test the deprecated API WithCompressor and WithDecompressor.
	serverCompression           bool
	unknownHandler              grpc.StreamHandler
	unaryServerInt              grpc.UnaryServerInterceptor
	streamServerInt             grpc.StreamServerInterceptor
	serverInitialWindowSize     int32
	serverInitialConnWindowSize int32
	customServerOptions         []grpc.ServerOption

	// The following knobs are for the client-side, and should be set after
	// calling newTest() and before calling clientConn().
	maxClientMsgSize        *int
	maxClientReceiveMsgSize *int
	maxClientSendMsgSize    *int
	maxClientHeaderListSize *uint32
	userAgent               string
	// Used to test the deprecated API WithCompressor and WithDecompressor.
	clientCompression bool
	// Used to test the new compressor registration API UseCompressor.
	clientUseCompression bool
	// clientNopCompression is set to create a compressor whose type is not supported.
	clientNopCompression        bool
	unaryClientInt              grpc.UnaryClientInterceptor
	streamClientInt             grpc.StreamClientInterceptor
	sc                          <-chan grpc.ServiceConfig
	customCodec                 encoding.Codec
	clientInitialWindowSize     int32
	clientInitialConnWindowSize int32
	perRPCCreds                 credentials.PerRPCCredentials
	customDialOptions           []grpc.DialOption
	resolverScheme              string

	// These are are set once startServer is called. The common case is to have
	// only one testServer.
	srv     stopper
	hSrv    healthgrpc.HealthServer
	srvAddr string

	// These are are set once startServers is called.
	srvs     []stopper
	hSrvs    []healthgrpc.HealthServer
	srvAddrs []string

	cc          *grpc.ClientConn // nil until requested via clientConn
	restoreLogs func()           // nil unless declareLogNoise is used
}

type stopper interface {
	Stop()
	GracefulStop()
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

	if te.restoreLogs != nil {
		te.restoreLogs()
		te.restoreLogs = nil
	}

	if te.srv != nil {
		te.srv.Stop()
	}
	for _, s := range te.srvs {
		s.Stop()
	}
}

// newTest returns a new test using the provided testing.T and
// environment.  It is returned with default values. Tests should
// modify it before calling its startServer and clientConn methods.
func newTest(t *testing.T, e env) *test {
	te := &test{
		t:         t,
		e:         e,
		maxStream: math.MaxUint32,
	}
	te.ctx, te.cancel = context.WithCancel(context.Background())
	return te
}

func (te *test) listenAndServe(ts testpb.TestServiceServer, listen func(network, address string) (net.Listener, error)) net.Listener {
	te.t.Helper()
	te.t.Logf("Running test in %s environment...", te.e.name)
	sopts := []grpc.ServerOption{grpc.MaxConcurrentStreams(te.maxStream)}
	if te.maxServerMsgSize != nil {
		sopts = append(sopts, grpc.MaxMsgSize(*te.maxServerMsgSize))
	}
	if te.maxServerReceiveMsgSize != nil {
		sopts = append(sopts, grpc.MaxRecvMsgSize(*te.maxServerReceiveMsgSize))
	}
	if te.maxServerSendMsgSize != nil {
		sopts = append(sopts, grpc.MaxSendMsgSize(*te.maxServerSendMsgSize))
	}
	if te.maxServerHeaderListSize != nil {
		sopts = append(sopts, grpc.MaxHeaderListSize(*te.maxServerHeaderListSize))
	}
	if te.tapHandle != nil {
		sopts = append(sopts, grpc.InTapHandle(te.tapHandle))
	}
	if te.serverCompression {
		sopts = append(sopts,
			grpc.RPCCompressor(grpc.NewGZIPCompressor()),
			grpc.RPCDecompressor(grpc.NewGZIPDecompressor()),
		)
	}
	if te.unaryServerInt != nil {
		sopts = append(sopts, grpc.UnaryInterceptor(te.unaryServerInt))
	}
	if te.streamServerInt != nil {
		sopts = append(sopts, grpc.StreamInterceptor(te.streamServerInt))
	}
	if te.unknownHandler != nil {
		sopts = append(sopts, grpc.UnknownServiceHandler(te.unknownHandler))
	}
	if te.serverInitialWindowSize > 0 {
		sopts = append(sopts, grpc.InitialWindowSize(te.serverInitialWindowSize))
	}
	if te.serverInitialConnWindowSize > 0 {
		sopts = append(sopts, grpc.InitialConnWindowSize(te.serverInitialConnWindowSize))
	}
	la := "localhost:0"
	switch te.e.network {
	case "unix":
		la = "/tmp/testsock" + fmt.Sprintf("%d", time.Now().UnixNano())
		syscall.Unlink(la)
	}
	lis, err := listen(te.e.network, la)
	if err != nil {
		te.t.Fatalf("Failed to listen: %v", err)
	}
	if te.e.security == "tls" {
		creds, err := credentials.NewServerTLSFromFile(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
		if err != nil {
			te.t.Fatalf("Failed to generate credentials %v", err)
		}
		sopts = append(sopts, grpc.Creds(creds))
	}
	sopts = append(sopts, te.customServerOptions...)
	s := grpc.NewServer(sopts...)
	if ts != nil {
		testpb.RegisterTestServiceServer(s, ts)
	}

	// Create a new default health server if enableHealthServer is set, or use
	// the provided one.
	hs := te.healthServer
	if te.enableHealthServer {
		hs = health.NewServer()
	}
	if hs != nil {
		healthgrpc.RegisterHealthServer(s, hs)
	}

	addr := la
	switch te.e.network {
	case "unix":
	default:
		_, port, err := net.SplitHostPort(lis.Addr().String())
		if err != nil {
			te.t.Fatalf("Failed to parse listener address: %v", err)
		}
		addr = "localhost:" + port
	}

	te.srv = s
	te.hSrv = hs
	te.srvAddr = addr

	if te.e.httpHandler {
		if te.e.security != "tls" {
			te.t.Fatalf("unsupported environment settings")
		}
		cert, err := tls.LoadX509KeyPair(testdata.Path("x509/server1_cert.pem"), testdata.Path("x509/server1_key.pem"))
		if err != nil {
			te.t.Fatal("tls.LoadX509KeyPair(server1.pem, server1.key) failed: ", err)
		}
		hs := &http.Server{
			Handler:   s,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		}
		if err := http2.ConfigureServer(hs, &http2.Server{MaxConcurrentStreams: te.maxStream}); err != nil {
			te.t.Fatal("http2.ConfigureServer(_, _) failed: ", err)
		}
		te.srv = wrapHS{hs}
		tlsListener := tls.NewListener(lis, hs.TLSConfig)
		go hs.Serve(tlsListener)
		return lis
	}

	go s.Serve(lis)
	return lis
}

type wrapHS struct {
	s *http.Server
}

func (w wrapHS) GracefulStop() {
	w.s.Shutdown(context.Background())
}

func (w wrapHS) Stop() {
	w.s.Close()
}

func (te *test) startServerWithConnControl(ts testpb.TestServiceServer) *listenerWrapper {
	l := te.listenAndServe(ts, listenWithConnControl)
	return l.(*listenerWrapper)
}

// startServer starts a gRPC server exposing the provided TestService
// implementation. Callers should defer a call to te.tearDown to clean up
func (te *test) startServer(ts testpb.TestServiceServer) {
	te.t.Helper()
	te.listenAndServe(ts, net.Listen)
}

// startServers starts 'num' gRPC servers exposing the provided TestService.
func (te *test) startServers(ts testpb.TestServiceServer, num int) {
	for i := 0; i < num; i++ {
		te.startServer(ts)
		te.srvs = append(te.srvs, te.srv.(*grpc.Server))
		te.hSrvs = append(te.hSrvs, te.hSrv)
		te.srvAddrs = append(te.srvAddrs, te.srvAddr)
		te.srv = nil
		te.hSrv = nil
		te.srvAddr = ""
	}
}

// setHealthServingStatus is a helper function to set the health status.
func (te *test) setHealthServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	hs, ok := te.hSrv.(*health.Server)
	if !ok {
		panic(fmt.Sprintf("SetServingStatus(%v, %v) called for health server of type %T", service, status, hs))
	}
	hs.SetServingStatus(service, status)
}

type nopCompressor struct {
	grpc.Compressor
}

// NewNopCompressor creates a compressor to test the case that type is not supported.
func NewNopCompressor() grpc.Compressor {
	return &nopCompressor{grpc.NewGZIPCompressor()}
}

func (c *nopCompressor) Type() string {
	return "nop"
}

type nopDecompressor struct {
	grpc.Decompressor
}

// NewNopDecompressor creates a decompressor to test the case that type is not supported.
func NewNopDecompressor() grpc.Decompressor {
	return &nopDecompressor{grpc.NewGZIPDecompressor()}
}

func (d *nopDecompressor) Type() string {
	return "nop"
}

func (te *test) configDial(opts ...grpc.DialOption) ([]grpc.DialOption, string) {
	opts = append(opts, grpc.WithDialer(te.e.dialer), grpc.WithUserAgent(te.userAgent))

	if te.sc != nil {
		opts = append(opts, grpc.WithServiceConfig(te.sc))
	}

	if te.clientCompression {
		opts = append(opts,
			grpc.WithCompressor(grpc.NewGZIPCompressor()),
			grpc.WithDecompressor(grpc.NewGZIPDecompressor()),
		)
	}
	if te.clientUseCompression {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")))
	}
	if te.clientNopCompression {
		opts = append(opts,
			grpc.WithCompressor(NewNopCompressor()),
			grpc.WithDecompressor(NewNopDecompressor()),
		)
	}
	if te.unaryClientInt != nil {
		opts = append(opts, grpc.WithUnaryInterceptor(te.unaryClientInt))
	}
	if te.streamClientInt != nil {
		opts = append(opts, grpc.WithStreamInterceptor(te.streamClientInt))
	}
	if te.maxClientMsgSize != nil {
		opts = append(opts, grpc.WithMaxMsgSize(*te.maxClientMsgSize))
	}
	if te.maxClientReceiveMsgSize != nil {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(*te.maxClientReceiveMsgSize)))
	}
	if te.maxClientSendMsgSize != nil {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(*te.maxClientSendMsgSize)))
	}
	if te.maxClientHeaderListSize != nil {
		opts = append(opts, grpc.WithMaxHeaderListSize(*te.maxClientHeaderListSize))
	}
	switch te.e.security {
	case "tls":
		creds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
		if err != nil {
			te.t.Fatalf("Failed to load credentials: %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case "empty":
		// Don't add any transport creds option.
	default:
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	// TODO(bar) switch balancer case "pick_first".
	var scheme string
	if te.resolverScheme == "" {
		scheme = "passthrough:///"
	} else {
		scheme = te.resolverScheme + ":///"
	}
	if te.e.balancer != "" {
		opts = append(opts, grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingConfig": [{"%s":{}}]}`, te.e.balancer)))
	}
	if te.clientInitialWindowSize > 0 {
		opts = append(opts, grpc.WithInitialWindowSize(te.clientInitialWindowSize))
	}
	if te.clientInitialConnWindowSize > 0 {
		opts = append(opts, grpc.WithInitialConnWindowSize(te.clientInitialConnWindowSize))
	}
	if te.perRPCCreds != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(te.perRPCCreds))
	}
	if te.customCodec != nil {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.ForceCodec(te.customCodec)))
	}
	if te.srvAddr == "" {
		te.srvAddr = "client.side.only.test"
	}
	opts = append(opts, te.customDialOptions...)
	return opts, scheme
}

func (te *test) clientConnWithConnControl() (*grpc.ClientConn, *dialerWrapper) {
	if te.cc != nil {
		return te.cc, nil
	}
	opts, scheme := te.configDial()
	dw := &dialerWrapper{}
	// overwrite the dialer before
	opts = append(opts, grpc.WithDialer(dw.dialer))
	var err error
	te.cc, err = grpc.Dial(scheme+te.srvAddr, opts...)
	if err != nil {
		te.t.Fatalf("Dial(%q) = %v", scheme+te.srvAddr, err)
	}
	return te.cc, dw
}

func (te *test) clientConn(opts ...grpc.DialOption) *grpc.ClientConn {
	if te.cc != nil {
		return te.cc
	}
	var scheme string
	opts, scheme = te.configDial(opts...)
	var err error
	te.cc, err = grpc.Dial(scheme+te.srvAddr, opts...)
	if err != nil {
		te.t.Fatalf("Dial(%q) = %v", scheme+te.srvAddr, err)
	}
	return te.cc
}

func (te *test) declareLogNoise(phrases ...string) {
	te.restoreLogs = declareLogNoise(te.t, phrases...)
}

func (te *test) withServerTester(fn func(st *serverTester)) {
	c, err := te.e.dialer(te.srvAddr, 10*time.Second)
	if err != nil {
		te.t.Fatal(err)
	}
	defer c.Close()
	if te.e.security == "tls" {
		c = tls.Client(c, &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{http2.NextProtoTLS},
		})
	}
	st := newServerTesterFromConn(te.t, c)
	st.greet()
	fn(st)
}

type lazyConn struct {
	net.Conn
	beLazy int32
}

func (l *lazyConn) Write(b []byte) (int, error) {
	if atomic.LoadInt32(&(l.beLazy)) == 1 {
		time.Sleep(time.Second)
	}
	return l.Conn.Write(b)
}

func (s) TestContextDeadlineNotIgnored(t *testing.T) {
	e := noBalancerEnv
	var lc *lazyConn
	e.customDialer = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		conn, err := net.DialTimeout(network, addr, timeout)
		if err != nil {
			return nil, err
		}
		lc = &lazyConn{Conn: conn}
		return lc, nil
	}

	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	cancel()
	atomic.StoreInt32(&(lc.beLazy), 1)
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	t1 := time.Now()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, context.DeadlineExceeded", err)
	}
	if time.Since(t1) > 2*time.Second {
		t.Fatalf("TestService/EmptyCall(_, _) ran over the deadline")
	}
}

func (s) TestTimeoutOnDeadServer(t *testing.T) {
	for _, e := range listTestEnv() {
		testTimeoutOnDeadServer(t, e)
	}
}

func testTimeoutOnDeadServer(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	te.srv.Stop()
	cancel()

	// Wait for the client to notice the connection is gone.
	ctx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	state := cc.GetState()
	for ; state == connectivity.Ready && cc.WaitForStateChange(ctx, state); state = cc.GetState() {
	}
	cancel()
	if state == connectivity.Ready {
		t.Fatalf("Timed out waiting for non-ready state")
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	_, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true))
	cancel()
	if e.balancer != "" && status.Code(err) != codes.DeadlineExceeded {
		// If e.balancer == nil, the ac will stop reconnecting because the dialer returns non-temp error,
		// the error will be an internal error.
		t.Fatalf("TestService/EmptyCall(%v, _) = _, %v, want _, error code: %s", ctx, err, codes.DeadlineExceeded)
	}
	awaitNewConnLogOutput()
}

func (s) TestServerGracefulStopIdempotent(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testServerGracefulStopIdempotent(t, e)
	}
}

func testServerGracefulStopIdempotent(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	for i := 0; i < 3; i++ {
		te.srv.GracefulStop()
	}
}

func (s) TestServerGoAway(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testServerGoAway(t, e)
	}
}

func testServerGoAway(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	// Finish an RPC to make sure the connection is good.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	ch := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(ch)
	}()
	// Loop until the server side GoAway signal is propagated to the client.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil && status.Code(err) != codes.DeadlineExceeded {
			cancel()
			break
		}
		cancel()
	}
	// A new RPC should fail.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable && status.Code(err) != codes.Internal {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s or %s", err, codes.Unavailable, codes.Internal)
	}
	<-ch
	awaitNewConnLogOutput()
}

func (s) TestServerGoAwayPendingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testServerGoAwayPendingRPC(t, e)
	}
}

func testServerGoAwayPendingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	// Finish an RPC to make sure the connection is good.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("%v.EmptyCall(_, _, _) = _, %v, want _, <nil>", tc, err)
	}
	ch := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(ch)
	}()
	// Loop until the server side GoAway signal is propagated to the client.
	start := time.Now()
	errored := false
	for time.Since(start) < time.Second {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true))
		cancel()
		if err != nil {
			errored = true
			break
		}
	}
	if !errored {
		t.Fatalf("GoAway never received by client")
	}
	respParam := []*testpb.ResponseParameters{{Size: 1}}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(100))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
	// The RPC will run until canceled.
	cancel()
	<-ch
	awaitNewConnLogOutput()
}

func (s) TestServerMultipleGoAwayPendingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testServerMultipleGoAwayPendingRPC(t, e)
	}
}

func testServerMultipleGoAwayPendingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	// Finish an RPC to make sure the connection is good.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("%v.EmptyCall(_, _, _) = _, %v, want _, <nil>", tc, err)
	}
	ch1 := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(ch1)
	}()
	ch2 := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(ch2)
	}()
	// Loop until the server side GoAway signal is propagated to the client.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
			cancel()
			break
		}
		cancel()
	}
	select {
	case <-ch1:
		t.Fatal("GracefulStop() terminated early")
	case <-ch2:
		t.Fatal("GracefulStop() terminated early")
	default:
	}
	respParam := []*testpb.ResponseParameters{
		{
			Size: 1,
		},
	}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(100))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	// The existing RPC should be still good to proceed.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() = %v, want <nil>", stream, err)
	}
	<-ch1
	<-ch2
	cancel()
	awaitNewConnLogOutput()
}

func (s) TestConcurrentClientConnCloseAndServerGoAway(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testConcurrentClientConnCloseAndServerGoAway(t, e)
	}
}

func testConcurrentClientConnCloseAndServerGoAway(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("%v.EmptyCall(_, _, _) = _, %v, want _, <nil>", tc, err)
	}
	ch := make(chan struct{})
	// Close ClientConn and Server concurrently.
	go func() {
		te.srv.GracefulStop()
		close(ch)
	}()
	go func() {
		cc.Close()
	}()
	<-ch
}

func (s) TestConcurrentServerStopAndGoAway(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testConcurrentServerStopAndGoAway(t, e)
	}
}

func testConcurrentServerStopAndGoAway(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}

	// Finish an RPC to make sure the connection is good.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("%v.EmptyCall(_, _, _) = _, %v, want _, <nil>", tc, err)
	}

	ch := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(ch)
	}()
	// Loop until the server side GoAway signal is propagated to the client.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); err != nil {
			cancel()
			break
		}
		cancel()
	}
	// Stop the server and close all the connections.
	te.srv.Stop()
	respParam := []*testpb.ResponseParameters{
		{
			Size: 1,
		},
	}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(100))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	sendStart := time.Now()
	for {
		if err := stream.Send(req); err == io.EOF {
			// stream.Send should eventually send io.EOF
			break
		} else if err != nil {
			// Send should never return a transport-level error.
			t.Fatalf("stream.Send(%v) = %v; want <nil or io.EOF>", req, err)
		}
		if time.Since(sendStart) > 2*time.Second {
			t.Fatalf("stream.Send(_) did not return io.EOF after 2s")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := stream.Recv(); err == nil || err == io.EOF {
		t.Fatalf("%v.Recv() = _, %v, want _, <non-nil, non-EOF>", stream, err)
	}
	<-ch
	awaitNewConnLogOutput()
}

func (s) TestDetailedConnectionCloseErrorPropagatesToRpcError(t *testing.T) {
	rpcStartedOnServer := make(chan struct{})
	rpcDoneOnClient := make(chan struct{})
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			close(rpcStartedOnServer)
			<-rpcDoneOnClient
			return status.Error(codes.Internal, "arbitrary status")
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The precise behavior of this test is subject to raceyness around the timing of when TCP packets
	// are sent from client to server, and when we tell the server to stop, so we need to account for both
	// of these possible error messages:
	// 1) If the call to ss.S.Stop() causes the server's sockets to close while there's still in-fight
	//    data from the client on the TCP connection, then the kernel can send an RST back to the client (also
	//    see https://stackoverflow.com/questions/33053507/econnreset-in-send-linux-c). Note that while this
	//    condition is expected to be rare due to the rpcStartedOnServer synchronization, in theory it should
	//    be possible, e.g. if the client sends a BDP ping at the right time.
	// 2) If, for example, the call to ss.S.Stop() happens after the RPC headers have been received at the
	//    server, then the TCP connection can shutdown gracefully when the server's socket closes.
	const possibleConnResetMsg = "connection reset by peer"
	const possibleEOFMsg = "error reading from server: EOF"
	// Start an RPC. Then, while the RPC is still being accepted or handled at the server, abruptly
	// stop the server, killing the connection. The RPC error message should include details about the specific
	// connection error that was encountered.
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall = _, %v, want _, <nil>", ss.Client, err)
	}
	// Block until the RPC has been started on the server. This ensures that the ClientConn will find a healthy
	// connection for the RPC to go out on initially, and that the TCP connection will shut down strictly after
	// the RPC has been started on it.
	<-rpcStartedOnServer
	ss.S.Stop()
	if _, err := stream.Recv(); err == nil || (!strings.Contains(err.Error(), possibleConnResetMsg) && !strings.Contains(err.Error(), possibleEOFMsg)) {
		t.Fatalf("%v.Recv() = _, %v, want _, rpc error containing substring: %q OR %q", stream, err, possibleConnResetMsg, possibleEOFMsg)
	}
	close(rpcDoneOnClient)
}

func (s) TestDetailedGoawayErrorOnGracefulClosePropagatesToRPCError(t *testing.T) {
	rpcDoneOnClient := make(chan struct{})
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			<-rpcDoneOnClient
			return status.Error(codes.Internal, "arbitrary status")
		},
	}
	sopts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      time.Millisecond * 100,
			MaxConnectionAgeGrace: time.Millisecond,
		}),
	}
	if err := ss.Start(sopts); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall = _, %v, want _, <nil>", ss.Client, err)
	}
	const expectedErrorMessageSubstring = "received prior goaway: code: NO_ERROR"
	_, err = stream.Recv()
	close(rpcDoneOnClient)
	if err == nil || !strings.Contains(err.Error(), expectedErrorMessageSubstring) {
		t.Fatalf("%v.Recv() = _, %v, want _, rpc error containing substring: %q", stream, err, expectedErrorMessageSubstring)
	}
}

func (s) TestDetailedGoawayErrorOnAbruptClosePropagatesToRPCError(t *testing.T) {
	// set the min keepalive time very low so that this test can take
	// a reasonable amount of time
	prev := internal.KeepaliveMinPingTime
	internal.KeepaliveMinPingTime = time.Millisecond
	defer func() { internal.KeepaliveMinPingTime = prev }()

	rpcDoneOnClient := make(chan struct{})
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			<-rpcDoneOnClient
			return status.Error(codes.Internal, "arbitrary status")
		},
	}
	sopts := []grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime: time.Second * 1000, /* arbitrary, large value */
		}),
	}
	dopts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                time.Millisecond,   /* should trigger "too many pings" error quickly */
			Timeout:             time.Second * 1000, /* arbitrary, large value */
			PermitWithoutStream: false,
		}),
	}
	if err := ss.Start(sopts, dopts...); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall = _, %v, want _, <nil>", ss.Client, err)
	}
	const expectedErrorMessageSubstring = `received prior goaway: code: ENHANCE_YOUR_CALM, debug data: "too_many_pings"`
	_, err = stream.Recv()
	close(rpcDoneOnClient)
	if err == nil || !strings.Contains(err.Error(), expectedErrorMessageSubstring) {
		t.Fatalf("%v.Recv() = _, %v, want _, rpc error containing substring: |%v|", stream, err, expectedErrorMessageSubstring)
	}
}

func (s) TestClientConnCloseAfterGoAwayWithActiveStream(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testClientConnCloseAfterGoAwayWithActiveStream(t, e)
	}
}

func testClientConnCloseAfterGoAwayWithActiveStream(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := tc.FullDuplexCall(ctx); err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want _, <nil>", tc, err)
	}
	done := make(chan struct{})
	go func() {
		te.srv.GracefulStop()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cc.Close()
	timeout := time.NewTimer(time.Second)
	select {
	case <-done:
	case <-timeout.C:
		t.Fatalf("Test timed-out.")
	}
}

func (s) TestFailFast(t *testing.T) {
	for _, e := range listTestEnv() {
		testFailFast(t, e)
	}
}

func testFailFast(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	// Stop the server and tear down all the existing connections.
	te.srv.Stop()
	// Loop until the server teardown is propagated to the client.
	for {
		if err := ctx.Err(); err != nil {
			t.Fatalf("EmptyCall did not return UNAVAILABLE before timeout")
		}
		_, err := tc.EmptyCall(ctx, &testpb.Empty{})
		if status.Code(err) == codes.Unavailable {
			break
		}
		t.Logf("%v.EmptyCall(_, _) = _, %v", tc, err)
		time.Sleep(10 * time.Millisecond)
	}
	// The client keeps reconnecting and ongoing fail-fast RPCs should fail with code.Unavailable.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("TestService/EmptyCall(_, _, _) = _, %v, want _, error code: %s", err, codes.Unavailable)
	}
	if _, err := tc.StreamingInputCall(ctx); status.Code(err) != codes.Unavailable {
		t.Fatalf("TestService/StreamingInputCall(_) = _, %v, want _, error code: %s", err, codes.Unavailable)
	}

	awaitNewConnLogOutput()
}

func testServiceConfigSetup(t *testing.T, e env) *test {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	return te
}

func newBool(b bool) (a *bool) {
	return &b
}

func newInt(b int) (a *int) {
	return &b
}

func newDuration(b time.Duration) (a *time.Duration) {
	a = new(time.Duration)
	*a = b
	return
}

func (s) TestGetMethodConfig(t *testing.T) {
	te := testServiceConfigSetup(t, tcpClearRREnv)
	defer te.tearDown()
	r := manual.NewBuilderWithScheme("whatever")

	te.resolverScheme = r.Scheme()
	cc := te.clientConn(grpc.WithResolvers(r))
	addrs := []resolver.Address{{Addr: te.srvAddr}}
	r.UpdateState(resolver.State{
		Addresses: addrs,
		ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "EmptyCall"
                }
            ],
            "waitForReady": true,
            "timeout": ".001s"
        },
        {
            "name": [
                {
                    "service": "grpc.testing.TestService"
                }
            ],
            "waitForReady": false
        }
    ]
}`)})

	tc := testpb.NewTestServiceClient(cc)

	// Make sure service config has been processed by grpc.
	for {
		if cc.GetMethodConfig("/grpc.testing.TestService/EmptyCall").WaitForReady != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	var err error
	if _, err = tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}

	r.UpdateState(resolver.State{Addresses: addrs, ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "UnaryCall"
                }
            ],
            "waitForReady": true,
            "timeout": ".001s"
        },
        {
            "name": [
                {
                    "service": "grpc.testing.TestService"
                }
            ],
            "waitForReady": false
        }
    ]
}`)})

	// Make sure service config has been processed by grpc.
	for {
		if mc := cc.GetMethodConfig("/grpc.testing.TestService/EmptyCall"); mc.WaitForReady != nil && !*mc.WaitForReady {
			break
		}
		time.Sleep(time.Millisecond)
	}
	// The following RPCs are expected to become fail-fast.
	if _, err = tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.Unavailable)
	}
}

func (s) TestServiceConfigWaitForReady(t *testing.T) {
	te := testServiceConfigSetup(t, tcpClearRREnv)
	defer te.tearDown()
	r := manual.NewBuilderWithScheme("whatever")

	// Case1: Client API set failfast to be false, and service config set wait_for_ready to be false, Client API should win, and the rpc will wait until deadline exceeds.
	te.resolverScheme = r.Scheme()
	cc := te.clientConn(grpc.WithResolvers(r))
	addrs := []resolver.Address{{Addr: te.srvAddr}}
	r.UpdateState(resolver.State{
		Addresses: addrs,
		ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "EmptyCall"
                },
                {
                    "service": "grpc.testing.TestService",
                    "method": "FullDuplexCall"
                }
            ],
            "waitForReady": false,
            "timeout": ".001s"
        }
    ]
}`)})

	tc := testpb.NewTestServiceClient(cc)

	// Make sure service config has been processed by grpc.
	for {
		if cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").WaitForReady != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	var err error
	if _, err = tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	if _, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}

	// Generate a service config update.
	// Case2:Client API set failfast to be false, and service config set wait_for_ready to be true, and the rpc will wait until deadline exceeds.
	r.UpdateState(resolver.State{
		Addresses: addrs,
		ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "EmptyCall"
                },
                {
                    "service": "grpc.testing.TestService",
                    "method": "FullDuplexCall"
                }
            ],
            "waitForReady": true,
            "timeout": ".001s"
        }
    ]
}`)})

	// Wait for the new service config to take effect.
	for {
		if mc := cc.GetMethodConfig("/grpc.testing.TestService/EmptyCall"); mc.WaitForReady != nil && *mc.WaitForReady {
			break
		}
		time.Sleep(time.Millisecond)
	}
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	if _, err := tc.FullDuplexCall(ctx); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
}

func (s) TestServiceConfigTimeout(t *testing.T) {
	te := testServiceConfigSetup(t, tcpClearRREnv)
	defer te.tearDown()
	r := manual.NewBuilderWithScheme("whatever")

	// Case1: Client API sets timeout to be 1ns and ServiceConfig sets timeout to be 1hr. Timeout should be 1ns (min of 1ns and 1hr) and the rpc will wait until deadline exceeds.
	te.resolverScheme = r.Scheme()
	cc := te.clientConn(grpc.WithResolvers(r))
	addrs := []resolver.Address{{Addr: te.srvAddr}}
	r.UpdateState(resolver.State{
		Addresses: addrs,
		ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "EmptyCall"
                },
                {
                    "service": "grpc.testing.TestService",
                    "method": "FullDuplexCall"
                }
            ],
            "waitForReady": true,
            "timeout": "3600s"
        }
    ]
}`)})

	tc := testpb.NewTestServiceClient(cc)

	// Make sure service config has been processed by grpc.
	for {
		if cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").Timeout != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// The following RPCs are expected to become non-fail-fast ones with 1ns deadline.
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	if _, err = tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), time.Nanosecond)
	if _, err = tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
	cancel()

	// Generate a service config update.
	// Case2: Client API sets timeout to be 1hr and ServiceConfig sets timeout to be 1ns. Timeout should be 1ns (min of 1ns and 1hr) and the rpc will wait until deadline exceeds.
	r.UpdateState(resolver.State{
		Addresses: addrs,
		ServiceConfig: parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "EmptyCall"
                },
                {
                    "service": "grpc.testing.TestService",
                    "method": "FullDuplexCall"
                }
            ],
            "waitForReady": true,
            "timeout": ".000000001s"
        }
    ]
}`)})

	// Wait for the new service config to take effect.
	for {
		if mc := cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall"); mc.Timeout != nil && *mc.Timeout == time.Nanosecond {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	if _, err = tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	if _, err = tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
	cancel()
}

func (s) TestServiceConfigMaxMsgSize(t *testing.T) {
	e := tcpClearRREnv
	r := manual.NewBuilderWithScheme("whatever")

	// Setting up values and objects shared across all test cases.
	const smallSize = 1
	const largeSize = 1024
	const extraLargeSize = 2048

	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}
	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	extraLargePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, extraLargeSize)
	if err != nil {
		t.Fatal(err)
	}

	// Case1: sc set maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te1 := testServiceConfigSetup(t, e)
	defer te1.tearDown()

	te1.resolverScheme = r.Scheme()
	te1.startServer(&testServer{security: e.security})
	cc1 := te1.clientConn(grpc.WithResolvers(r))

	addrs := []resolver.Address{{Addr: te1.srvAddr}}
	sc := parseServiceConfig(t, r, `{
    "methodConfig": [
        {
            "name": [
                {
                    "service": "grpc.testing.TestService",
                    "method": "UnaryCall"
                },
                {
                    "service": "grpc.testing.TestService",
                    "method": "FullDuplexCall"
                }
            ],
            "maxRequestMessageBytes": 2048,
            "maxResponseMessageBytes": 2048
        }
    ]
}`)
	r.UpdateState(resolver.State{Addresses: addrs, ServiceConfig: sc})
	tc := testpb.NewTestServiceClient(cc1)

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(extraLargeSize),
		Payload:      smallPayload,
	}

	for {
		if cc1.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").MaxReqSize != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Test for unary RPC recv.
	if _, err = tc.UnaryCall(ctx, req, grpc.WaitForReady(true)); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = extraLargePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(extraLargeSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            smallPayload,
	}
	stream, err := tc.FullDuplexCall(te1.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err = stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = extraLargePayload
	stream, err = tc.FullDuplexCall(te1.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}

	// Case2: Client API set maxReqSize to 1024 (send), maxRespSize to 1024 (recv). Sc sets maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te2 := testServiceConfigSetup(t, e)
	te2.resolverScheme = r.Scheme()
	te2.maxClientReceiveMsgSize = newInt(1024)
	te2.maxClientSendMsgSize = newInt(1024)

	te2.startServer(&testServer{security: e.security})
	defer te2.tearDown()
	cc2 := te2.clientConn(grpc.WithResolvers(r))
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: te2.srvAddr}}, ServiceConfig: sc})
	tc = testpb.NewTestServiceClient(cc2)

	for {
		if cc2.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").MaxReqSize != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Test for unary RPC recv.
	req.Payload = smallPayload
	req.ResponseSize = int32(largeSize)

	if _, err = tc.UnaryCall(ctx, req, grpc.WaitForReady(true)); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	stream, err = tc.FullDuplexCall(te2.ctx)
	respParam[0].Size = int32(largeSize)
	sreq.Payload = smallPayload
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err = stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te2.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}

	// Case3: Client API set maxReqSize to 4096 (send), maxRespSize to 4096 (recv). Sc sets maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te3 := testServiceConfigSetup(t, e)
	te3.resolverScheme = r.Scheme()
	te3.maxClientReceiveMsgSize = newInt(4096)
	te3.maxClientSendMsgSize = newInt(4096)

	te3.startServer(&testServer{security: e.security})
	defer te3.tearDown()

	cc3 := te3.clientConn(grpc.WithResolvers(r))
	r.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: te3.srvAddr}}, ServiceConfig: sc})
	tc = testpb.NewTestServiceClient(cc3)

	for {
		if cc3.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").MaxReqSize != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Test for unary RPC recv.
	req.Payload = smallPayload
	req.ResponseSize = int32(largeSize)

	if _, err = tc.UnaryCall(ctx, req, grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want <nil>", err)
	}

	req.ResponseSize = int32(extraLargeSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want <nil>", err)
	}

	req.Payload = extraLargePayload
	if _, err = tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	stream, err = tc.FullDuplexCall(te3.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam[0].Size = int32(largeSize)
	sreq.Payload = smallPayload

	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err = stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want <nil>", stream, err)
	}

	respParam[0].Size = int32(extraLargeSize)

	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err = stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te3.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	sreq.Payload = extraLargePayload
	if err := stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}
}

// Reading from a streaming RPC may fail with context canceled if timeout was
// set by service config (https://github.com/grpc/grpc-go/issues/1818). This
// test makes sure read from streaming RPC doesn't fail in this case.
func (s) TestStreamingRPCWithTimeoutInServiceConfigRecv(t *testing.T) {
	te := testServiceConfigSetup(t, tcpClearRREnv)
	te.startServer(&testServer{security: tcpClearRREnv.security})
	defer te.tearDown()
	r := manual.NewBuilderWithScheme("whatever")

	te.resolverScheme = r.Scheme()
	cc := te.clientConn(grpc.WithResolvers(r))
	tc := testpb.NewTestServiceClient(cc)

	r.UpdateState(resolver.State{
		Addresses: []resolver.Address{{Addr: te.srvAddr}},
		ServiceConfig: parseServiceConfig(t, r, `{
	    "methodConfig": [
	        {
	            "name": [
	                {
	                    "service": "grpc.testing.TestService",
	                    "method": "FullDuplexCall"
	                }
	            ],
	            "waitForReady": true,
	            "timeout": "10s"
	        }
	    ]
	}`)})
	// Make sure service config has been processed by grpc.
	for {
		if cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall").Timeout != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want <nil>", err)
	}

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 0)
	if err != nil {
		t.Fatalf("failed to newPayload: %v", err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{{Size: 0}},
		Payload:            payload,
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("stream.Send(%v) = %v, want <nil>", req, err)
	}
	stream.CloseSend()
	time.Sleep(time.Second)
	// Sleep 1 second before recv to make sure the final status is received
	// before the recv.
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("stream.Recv = _, %v, want _, <nil>", err)
	}
	// Keep reading to drain the stream.
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
}

func (s) TestPreloaderClientSend(t *testing.T) {
	for _, e := range listTestEnv() {
		testPreloaderClientSend(t, e)
	}
}

func testPreloaderClientSend(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	te.startServer(&testServer{security: e.security})

	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	// Test for streaming RPC recv.
	// Set context for send with proper RPC Information
	stream, err := tc.FullDuplexCall(te.ctx, grpc.UseCompressor("gzip"))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	var index int
	for index < len(reqSizes) {
		respParam := []*testpb.ResponseParameters{
			{
				Size: int32(respSizes[index]),
			},
		}

		payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(reqSizes[index]))
		if err != nil {
			t.Fatal(err)
		}

		req := &testpb.StreamingOutputCallRequest{
			ResponseType:       testpb.PayloadType_COMPRESSABLE,
			ResponseParameters: respParam,
			Payload:            payload,
		}
		preparedMsg := &grpc.PreparedMsg{}
		err = preparedMsg.Encode(stream, req)
		if err != nil {
			t.Fatalf("PrepareMsg failed for size %d : %v", reqSizes[index], err)
		}
		if err := stream.SendMsg(preparedMsg); err != nil {
			t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
		}
		reply, err := stream.Recv()
		if err != nil {
			t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
		}
		pt := reply.GetPayload().GetType()
		if pt != testpb.PayloadType_COMPRESSABLE {
			t.Fatalf("Got the reply of type %d, want %d", pt, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			t.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v failed to complele the ping pong test: %v", stream, err)
	}
}

func (s) TestPreloaderSenderSend(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for i := 0; i < 10; i++ {
				preparedMsg := &grpc.PreparedMsg{}
				err := preparedMsg.Encode(stream, &testpb.StreamingOutputCallResponse{
					Payload: &testpb.Payload{
						Body: []byte{'0' + uint8(i)},
					},
				})
				if err != nil {
					return err
				}
				stream.SendMsg(preparedMsg)
			}
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	var ngot int
	var buf bytes.Buffer
	for {
		reply, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		ngot++
		if buf.Len() > 0 {
			buf.WriteByte(',')
		}
		buf.Write(reply.GetPayload().GetBody())
	}
	if want := 10; ngot != want {
		t.Errorf("Got %d replies, want %d", ngot, want)
	}
	if got, want := buf.String(), "0,1,2,3,4,5,6,7,8,9"; got != want {
		t.Errorf("Got replies %q; want %q", got, want)
	}
}

func (s) TestMaxMsgSizeClientDefault(t *testing.T) {
	for _, e := range listTestEnv() {
		testMaxMsgSizeClientDefault(t, e)
	}
}

func testMaxMsgSizeClientDefault(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	te.startServer(&testServer{security: e.security})

	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const smallSize = 1
	const largeSize = 4 * 1024 * 1024
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(largeSize),
		Payload:      smallPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Test for unary RPC recv.
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(largeSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            smallPayload,
	}

	// Test for streaming RPC recv.
	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
}

func (s) TestMaxMsgSizeClientAPI(t *testing.T) {
	for _, e := range listTestEnv() {
		testMaxMsgSizeClientAPI(t, e)
	}
}

func testMaxMsgSizeClientAPI(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	// To avoid error on server side.
	te.maxServerSendMsgSize = newInt(5 * 1024 * 1024)
	te.maxClientReceiveMsgSize = newInt(1024)
	te.maxClientSendMsgSize = newInt(1024)
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	te.startServer(&testServer{security: e.security})

	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const smallSize = 1
	const largeSize = 1024
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(largeSize),
		Payload:      smallPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Test for unary RPC recv.
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(largeSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            smallPayload,
	}

	// Test for streaming RPC recv.
	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}
}

func (s) TestMaxMsgSizeServerAPI(t *testing.T) {
	for _, e := range listTestEnv() {
		testMaxMsgSizeServerAPI(t, e)
	}
}

func testMaxMsgSizeServerAPI(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.maxServerReceiveMsgSize = newInt(1024)
	te.maxServerSendMsgSize = newInt(1024)
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	te.startServer(&testServer{security: e.security})

	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const smallSize = 1
	const largeSize = 1024
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(largeSize),
		Payload:      smallPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Test for unary RPC send.
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC recv.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(largeSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            smallPayload,
	}

	// Test for streaming RPC send.
	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
}

func (s) TestTap(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testTap(t, e)
	}
}

type myTap struct {
	cnt int
}

func (t *myTap) handle(ctx context.Context, info *tap.Info) (context.Context, error) {
	if info != nil {
		switch info.FullMethodName {
		case "/grpc.testing.TestService/EmptyCall":
			t.cnt++
		case "/grpc.testing.TestService/UnaryCall":
			return nil, fmt.Errorf("tap error")
		case "/grpc.testing.TestService/FullDuplexCall":
			return nil, status.Errorf(codes.FailedPrecondition, "test custom error")
		}
	}
	return ctx, nil
}

func testTap(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	ttap := &myTap{}
	te.tapHandle = ttap.handle
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
	)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	if ttap.cnt != 1 {
		t.Fatalf("Get the count in ttap %d, want 1", ttap.cnt)
	}

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 31)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: 45,
		Payload:      payload,
	}
	if _, err := tc.UnaryCall(ctx, req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, %s", err, codes.PermissionDenied)
	}
	str, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("Unexpected error creating stream: %v", err)
	}
	if _, err := str.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("FullDuplexCall Recv() = _, %v, want _, %s", err, codes.FailedPrecondition)
	}
}

// healthCheck is a helper function to make a unary health check RPC and return
// the response.
func healthCheck(d time.Duration, cc *grpc.ClientConn, service string) (*healthpb.HealthCheckResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	hc := healthgrpc.NewHealthClient(cc)
	return hc.Check(ctx, &healthpb.HealthCheckRequest{Service: service})
}

// verifyHealthCheckStatus is a helper function to verify that the current
// health status of the service matches the one passed in 'wantStatus'.
func verifyHealthCheckStatus(t *testing.T, d time.Duration, cc *grpc.ClientConn, service string, wantStatus healthpb.HealthCheckResponse_ServingStatus) {
	t.Helper()
	resp, err := healthCheck(d, cc, service)
	if err != nil {
		t.Fatalf("Health/Check(_, _) = _, %v, want _, <nil>", err)
	}
	if resp.Status != wantStatus {
		t.Fatalf("Got the serving status %v, want %v", resp.Status, wantStatus)
	}
}

// verifyHealthCheckErrCode is a helper function to verify that a unary health
// check RPC returns an error with a code set to 'wantCode'.
func verifyHealthCheckErrCode(t *testing.T, d time.Duration, cc *grpc.ClientConn, service string, wantCode codes.Code) {
	t.Helper()
	if _, err := healthCheck(d, cc, service); status.Code(err) != wantCode {
		t.Fatalf("Health/Check() got errCode %v, want %v", status.Code(err), wantCode)
	}
}

// newHealthCheckStream is a helper function to start a health check streaming
// RPC, and returns the stream.
func newHealthCheckStream(t *testing.T, cc *grpc.ClientConn, service string) (healthgrpc.Health_WatchClient, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	hc := healthgrpc.NewHealthClient(cc)
	stream, err := hc.Watch(ctx, &healthpb.HealthCheckRequest{Service: service})
	if err != nil {
		t.Fatalf("hc.Watch(_, %v) failed: %v", service, err)
	}
	return stream, cancel
}

// healthWatchChecker is a helper function to verify that the next health
// status returned on the given stream matches the one passed in 'wantStatus'.
func healthWatchChecker(t *testing.T, stream healthgrpc.Health_WatchClient, wantStatus healthpb.HealthCheckResponse_ServingStatus) {
	t.Helper()
	response, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv() failed: %v", err)
	}
	if response.Status != wantStatus {
		t.Fatalf("got servingStatus %v, want %v", response.Status, wantStatus)
	}
}

// TestHealthCheckSuccess invokes the unary Check() RPC on the health server in
// a successful case.
func (s) TestHealthCheckSuccess(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthCheckSuccess(t, e)
	}
}

func testHealthCheckSuccess(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	defer te.tearDown()

	verifyHealthCheckErrCode(t, 1*time.Second, te.clientConn(), defaultHealthService, codes.OK)
}

// TestHealthCheckFailure invokes the unary Check() RPC on the health server
// with an expired context and expects the RPC to fail.
func (s) TestHealthCheckFailure(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthCheckFailure(t, e)
	}
}

func testHealthCheckFailure(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise(
		"Failed to dial ",
		"grpc: the client connection is closing; please retry",
	)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	defer te.tearDown()

	verifyHealthCheckErrCode(t, 0*time.Second, te.clientConn(), defaultHealthService, codes.DeadlineExceeded)
	awaitNewConnLogOutput()
}

// TestHealthCheckOff makes a unary Check() RPC on the health server where the
// health status of the defaultHealthService is not set, and therefore expects
// an error code 'codes.NotFound'.
func (s) TestHealthCheckOff(t *testing.T) {
	for _, e := range listTestEnv() {
		// TODO(bradfitz): Temporarily skip this env due to #619.
		if e.name == "handler-tls" {
			continue
		}
		testHealthCheckOff(t, e)
	}
}

func testHealthCheckOff(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	verifyHealthCheckErrCode(t, 1*time.Second, te.clientConn(), defaultHealthService, codes.NotFound)
}

// TestHealthWatchMultipleClients makes a streaming Watch() RPC on the health
// server with multiple clients and expects the same status on both streams.
func (s) TestHealthWatchMultipleClients(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchMultipleClients(t, e)
	}
}

func testHealthWatchMultipleClients(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	stream1, cf1 := newHealthCheckStream(t, cc, defaultHealthService)
	defer cf1()
	healthWatchChecker(t, stream1, healthpb.HealthCheckResponse_SERVICE_UNKNOWN)

	stream2, cf2 := newHealthCheckStream(t, cc, defaultHealthService)
	defer cf2()
	healthWatchChecker(t, stream2, healthpb.HealthCheckResponse_SERVICE_UNKNOWN)

	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_NOT_SERVING)
	healthWatchChecker(t, stream1, healthpb.HealthCheckResponse_NOT_SERVING)
	healthWatchChecker(t, stream2, healthpb.HealthCheckResponse_NOT_SERVING)
}

// TestHealthWatchSameStatusmakes a streaming Watch() RPC on the health server
// and makes sure that the health status of the server is as expected after
// multiple calls to SetServingStatus with the same status.
func (s) TestHealthWatchSameStatus(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchSameStatus(t, e)
	}
}

func testHealthWatchSameStatus(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	stream, cf := newHealthCheckStream(t, te.clientConn(), defaultHealthService)
	defer cf()

	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVING)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_NOT_SERVING)
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_NOT_SERVING)
}

// TestHealthWatchServiceStatusSetBeforeStartingServer starts a health server
// on which the health status for the defaultService is set before the gRPC
// server is started, and expects the correct health status to be returned.
func (s) TestHealthWatchServiceStatusSetBeforeStartingServer(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchSetServiceStatusBeforeStartingServer(t, e)
	}
}

func testHealthWatchSetServiceStatusBeforeStartingServer(t *testing.T, e env) {
	hs := health.NewServer()
	te := newTest(t, e)
	te.healthServer = hs
	hs.SetServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	stream, cf := newHealthCheckStream(t, te.clientConn(), defaultHealthService)
	defer cf()
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVING)
}

// TestHealthWatchDefaultStatusChange verifies the simple case where the
// service starts off with a SERVICE_UNKNOWN status (because SetServingStatus
// hasn't been called yet) and then moves to SERVING after SetServingStatus is
// called.
func (s) TestHealthWatchDefaultStatusChange(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchDefaultStatusChange(t, e)
	}
}

func testHealthWatchDefaultStatusChange(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	stream, cf := newHealthCheckStream(t, te.clientConn(), defaultHealthService)
	defer cf()
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVICE_UNKNOWN)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVING)
}

// TestHealthWatchSetServiceStatusBeforeClientCallsWatch verifies the case
// where the health status is set to SERVING before the client calls Watch().
func (s) TestHealthWatchSetServiceStatusBeforeClientCallsWatch(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchSetServiceStatusBeforeClientCallsWatch(t, e)
	}
}

func testHealthWatchSetServiceStatusBeforeClientCallsWatch(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	defer te.tearDown()

	stream, cf := newHealthCheckStream(t, te.clientConn(), defaultHealthService)
	defer cf()
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVING)
}

// TestHealthWatchOverallServerHealthChange verifies setting the overall status
// of the server by using the empty service name.
func (s) TestHealthWatchOverallServerHealthChange(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthWatchOverallServerHealthChange(t, e)
	}
}

func testHealthWatchOverallServerHealthChange(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	stream, cf := newHealthCheckStream(t, te.clientConn(), "")
	defer cf()
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_SERVING)
	te.setHealthServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthWatchChecker(t, stream, healthpb.HealthCheckResponse_NOT_SERVING)
}

// TestUnknownHandler verifies that an expected error is returned (by setting
// the unknownHandler on the server) for a service which is not exposed to the
// client.
func (s) TestUnknownHandler(t *testing.T) {
	// An example unknownHandler that returns a different code and a different
	// method, making sure that we do not expose what methods are implemented to
	// a client that is not authenticated.
	unknownHandler := func(srv interface{}, stream grpc.ServerStream) error {
		return status.Error(codes.Unauthenticated, "user unauthenticated")
	}
	for _, e := range listTestEnv() {
		// TODO(bradfitz): Temporarily skip this env due to #619.
		if e.name == "handler-tls" {
			continue
		}
		testUnknownHandler(t, e, unknownHandler)
	}
}

func testUnknownHandler(t *testing.T, e env, unknownHandler grpc.StreamHandler) {
	te := newTest(t, e)
	te.unknownHandler = unknownHandler
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	verifyHealthCheckErrCode(t, 1*time.Second, te.clientConn(), "", codes.Unauthenticated)
}

// TestHealthCheckServingStatus makes a streaming Watch() RPC on the health
// server and verifies a bunch of health status transitions.
func (s) TestHealthCheckServingStatus(t *testing.T) {
	for _, e := range listTestEnv() {
		testHealthCheckServingStatus(t, e)
	}
}

func testHealthCheckServingStatus(t *testing.T, e env) {
	te := newTest(t, e)
	te.enableHealthServer = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	verifyHealthCheckStatus(t, 1*time.Second, cc, "", healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckErrCode(t, 1*time.Second, cc, defaultHealthService, codes.NotFound)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_SERVING)
	te.setHealthServingStatus(defaultHealthService, healthpb.HealthCheckResponse_NOT_SERVING)
	verifyHealthCheckStatus(t, 1*time.Second, cc, defaultHealthService, healthpb.HealthCheckResponse_NOT_SERVING)
}

func (s) TestEmptyUnaryWithUserAgent(t *testing.T) {
	for _, e := range listTestEnv() {
		testEmptyUnaryWithUserAgent(t, e)
	}
}

func testEmptyUnaryWithUserAgent(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	var header metadata.MD
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	reply, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.Header(&header))
	if err != nil || !proto.Equal(&testpb.Empty{}, reply) {
		t.Fatalf("TestService/EmptyCall(_, _) = %v, %v, want %v, <nil>", reply, err, &testpb.Empty{})
	}
	if v, ok := header["ua"]; !ok || !strings.HasPrefix(v[0], testAppUA) {
		t.Fatalf("header[\"ua\"] = %q, %t, want string with prefix %q, true", v, ok, testAppUA)
	}

	te.srv.Stop()
}

func (s) TestFailedEmptyUnary(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			// This test covers status details, but
			// Grpc-Status-Details-Bin is not support in handler_server.
			continue
		}
		testFailedEmptyUnary(t, e)
	}
}

func testFailedEmptyUnary(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = failAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	wantErr := detailedError
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); !testutils.StatusErrEqual(err, wantErr) {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %v", err, wantErr)
	}
}

func (s) TestLargeUnary(t *testing.T) {
	for _, e := range listTestEnv() {
		testLargeUnary(t, e)
	}
}

func testLargeUnary(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const argSize = 271828
	const respSize = 314159

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	reply, err := tc.UnaryCall(ctx, req)
	if err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, <nil>", err)
	}
	pt := reply.GetPayload().GetType()
	ps := len(reply.GetPayload().GetBody())
	if pt != testpb.PayloadType_COMPRESSABLE || ps != respSize {
		t.Fatalf("Got the reply with type %d len %d; want %d, %d", pt, ps, testpb.PayloadType_COMPRESSABLE, respSize)
	}
}

// Test backward-compatibility API for setting msg size limit.
func (s) TestExceedMsgLimit(t *testing.T) {
	for _, e := range listTestEnv() {
		testExceedMsgLimit(t, e)
	}
}

func testExceedMsgLimit(t *testing.T, e env) {
	te := newTest(t, e)
	maxMsgSize := 1024
	te.maxServerMsgSize, te.maxClientMsgSize = newInt(maxMsgSize), newInt(maxMsgSize)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	largeSize := int32(maxMsgSize + 1)
	const smallSize = 1

	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure the server cannot receive a unary RPC of largeSize.
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: smallSize,
		Payload:      largePayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}
	// Make sure the client cannot receive a unary RPC of largeSize.
	req.ResponseSize = largeSize
	req.Payload = smallPayload
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Make sure the server cannot receive a streaming RPC of largeSize.
	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam := []*testpb.ResponseParameters{
		{
			Size: 1,
		},
	}

	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            largePayload,
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test on client side for streaming RPC.
	stream, err = tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam[0].Size = largeSize
	sreq.Payload = smallPayload
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
}

func (s) TestPeerClientSide(t *testing.T) {
	for _, e := range listTestEnv() {
		testPeerClientSide(t, e)
	}
}

func testPeerClientSide(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())
	peer := new(peer.Peer)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
	pa := peer.Addr.String()
	if e.network == "unix" {
		if pa != te.srvAddr {
			t.Fatalf("peer.Addr = %v, want %v", pa, te.srvAddr)
		}
		return
	}
	_, pp, err := net.SplitHostPort(pa)
	if err != nil {
		t.Fatalf("Failed to parse address from peer.")
	}
	_, sp, err := net.SplitHostPort(te.srvAddr)
	if err != nil {
		t.Fatalf("Failed to parse address of test server.")
	}
	if pp != sp {
		t.Fatalf("peer.Addr = localhost:%v, want localhost:%v", pp, sp)
	}
}

// TestPeerNegative tests that if call fails setting peer
// doesn't cause a segmentation fault.
// issue#1141 https://github.com/grpc/grpc-go/issues/1141
func (s) TestPeerNegative(t *testing.T) {
	for _, e := range listTestEnv() {
		testPeerNegative(t, e)
	}
}

func testPeerNegative(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	peer := new(peer.Peer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tc.EmptyCall(ctx, &testpb.Empty{}, grpc.Peer(peer))
}

func (s) TestPeerFailedRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testPeerFailedRPC(t, e)
	}
}

func testPeerFailedRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.maxServerReceiveMsgSize = newInt(1 * 1024)
	te.startServer(&testServer{security: e.security})

	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// first make a successful request to the server
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}

	// make a second request that will be rejected by the server
	const largeSize = 5 * 1024
	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		Payload:      largePayload,
	}

	peer := new(peer.Peer)
	if _, err := tc.UnaryCall(ctx, req, grpc.Peer(peer)); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	} else {
		pa := peer.Addr.String()
		if e.network == "unix" {
			if pa != te.srvAddr {
				t.Fatalf("peer.Addr = %v, want %v", pa, te.srvAddr)
			}
			return
		}
		_, pp, err := net.SplitHostPort(pa)
		if err != nil {
			t.Fatalf("Failed to parse address from peer.")
		}
		_, sp, err := net.SplitHostPort(te.srvAddr)
		if err != nil {
			t.Fatalf("Failed to parse address of test server.")
		}
		if pp != sp {
			t.Fatalf("peer.Addr = localhost:%v, want localhost:%v", pp, sp)
		}
	}
}

func (s) TestMetadataUnaryRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testMetadataUnaryRPC(t, e)
	}
}

func testMetadataUnaryRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const argSize = 2718
	const respSize = 314

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	var header, trailer metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	if _, err := tc.UnaryCall(ctx, req, grpc.Header(&header), grpc.Trailer(&trailer)); err != nil {
		t.Fatalf("TestService.UnaryCall(%v, _, _, _) = _, %v; want _, <nil>", ctx, err)
	}
	// Ignore optional response headers that Servers may set:
	if header != nil {
		delete(header, "trailer") // RFC 2616 says server SHOULD (but optional) declare trailers
		delete(header, "date")    // the Date header is also optional
		delete(header, "user-agent")
		delete(header, "content-type")
	}
	if !reflect.DeepEqual(header, testMetadata) {
		t.Fatalf("Received header metadata %v, want %v", header, testMetadata)
	}
	if !reflect.DeepEqual(trailer, testTrailerMetadata) {
		t.Fatalf("Received trailer metadata %v, want %v", trailer, testTrailerMetadata)
	}
}

func (s) TestMetadataOrderUnaryRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testMetadataOrderUnaryRPC(t, e)
	}
}

func testMetadataOrderUnaryRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	ctx = metadata.AppendToOutgoingContext(ctx, "key1", "value2")
	ctx = metadata.AppendToOutgoingContext(ctx, "key1", "value3")

	// using Join to built expected metadata instead of FromOutgoingContext
	newMetadata := metadata.Join(testMetadata, metadata.Pairs("key1", "value2", "key1", "value3"))

	var header metadata.MD
	if _, err := tc.UnaryCall(ctx, &testpb.SimpleRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}

	// Ignore optional response headers that Servers may set:
	if header != nil {
		delete(header, "trailer") // RFC 2616 says server SHOULD (but optional) declare trailers
		delete(header, "date")    // the Date header is also optional
		delete(header, "user-agent")
		delete(header, "content-type")
	}

	if !reflect.DeepEqual(header, newMetadata) {
		t.Fatalf("Received header metadata %v, want %v", header, newMetadata)
	}
}

func (s) TestMultipleSetTrailerUnaryRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testMultipleSetTrailerUnaryRPC(t, e)
	}
}

func testMultipleSetTrailerUnaryRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, multipleSetTrailer: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = 1
	)
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	var trailer metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	if _, err := tc.UnaryCall(ctx, req, grpc.Trailer(&trailer), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService.UnaryCall(%v, _, _, _) = _, %v; want _, <nil>", ctx, err)
	}
	expectedTrailer := metadata.Join(testTrailerMetadata, testTrailerMetadata2)
	if !reflect.DeepEqual(trailer, expectedTrailer) {
		t.Fatalf("Received trailer metadata %v, want %v", trailer, expectedTrailer)
	}
}

func (s) TestMultipleSetTrailerStreamingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testMultipleSetTrailerStreamingRPC(t, e)
	}
}

func testMultipleSetTrailerStreamingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, multipleSetTrailer: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	stream, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v failed to complele the FullDuplexCall: %v", stream, err)
	}

	trailer := stream.Trailer()
	expectedTrailer := metadata.Join(testTrailerMetadata, testTrailerMetadata2)
	if !reflect.DeepEqual(trailer, expectedTrailer) {
		t.Fatalf("Received trailer metadata %v, want %v", trailer, expectedTrailer)
	}
}

func (s) TestSetAndSendHeaderUnaryRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testSetAndSendHeaderUnaryRPC(t, e)
	}
}

// To test header metadata is sent on SendHeader().
func testSetAndSendHeaderUnaryRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setAndSendHeader: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = 1
	)
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	var header metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	if _, err := tc.UnaryCall(ctx, req, grpc.Header(&header), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService.UnaryCall(%v, _, _, _) = _, %v; want _, <nil>", ctx, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}
}

func (s) TestMultipleSetHeaderUnaryRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testMultipleSetHeaderUnaryRPC(t, e)
	}
}

// To test header metadata is sent when sending response.
func testMultipleSetHeaderUnaryRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setHeaderOnly: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = 1
	)
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}

	var header metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	if _, err := tc.UnaryCall(ctx, req, grpc.Header(&header), grpc.WaitForReady(true)); err != nil {
		t.Fatalf("TestService.UnaryCall(%v, _, _, _) = _, %v; want _, <nil>", ctx, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}
}

func (s) TestMultipleSetHeaderUnaryRPCError(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testMultipleSetHeaderUnaryRPCError(t, e)
	}
}

// To test header metadata is sent when sending status.
func testMultipleSetHeaderUnaryRPCError(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setHeaderOnly: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = -1 // Invalid respSize to make RPC fail.
	)
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	var header metadata.MD
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	if _, err := tc.UnaryCall(ctx, req, grpc.Header(&header), grpc.WaitForReady(true)); err == nil {
		t.Fatalf("TestService.UnaryCall(%v, _, _, _) = _, %v; want _, <non-nil>", ctx, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}
}

func (s) TestSetAndSendHeaderStreamingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testSetAndSendHeaderStreamingRPC(t, e)
	}
}

// To test header metadata is sent on SendHeader().
func testSetAndSendHeaderStreamingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setAndSendHeader: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v failed to complele the FullDuplexCall: %v", stream, err)
	}

	header, err := stream.Header()
	if err != nil {
		t.Fatalf("%v.Header() = _, %v, want _, <nil>", stream, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}
}

func (s) TestMultipleSetHeaderStreamingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testMultipleSetHeaderStreamingRPC(t, e)
	}
}

// To test header metadata is sent when sending response.
func testMultipleSetHeaderStreamingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setHeaderOnly: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = 1
	)
	ctx := metadata.NewOutgoingContext(context.Background(), testMetadata)
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.StreamingOutputCallRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{
			{Size: respSize},
		},
		Payload: payload,
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v failed to complele the FullDuplexCall: %v", stream, err)
	}

	header, err := stream.Header()
	if err != nil {
		t.Fatalf("%v.Header() = _, %v, want _, <nil>", stream, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}

}

func (s) TestMultipleSetHeaderStreamingRPCError(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testMultipleSetHeaderStreamingRPCError(t, e)
	}
}

// To test header metadata is sent when sending status.
func testMultipleSetHeaderStreamingRPCError(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, setHeaderOnly: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const (
		argSize  = 1
		respSize = -1
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, testMetadata)
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.StreamingOutputCallRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{
			{Size: respSize},
		},
		Payload: payload,
	}
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatalf("%v.Recv() = %v, want <non-nil>", stream, err)
	}

	header, err := stream.Header()
	if err != nil {
		t.Fatalf("%v.Header() = _, %v, want _, <nil>", stream, err)
	}
	delete(header, "user-agent")
	delete(header, "content-type")
	expectedHeader := metadata.Join(testMetadata, testMetadata2)
	if !reflect.DeepEqual(header, expectedHeader) {
		t.Fatalf("Received header metadata %v, want %v", header, expectedHeader)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
}

// TestMalformedHTTP2Metadata verfies the returned error when the client
// sends an illegal metadata.
func (s) TestMalformedHTTP2Metadata(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			// Failed with "server stops accepting new RPCs".
			// Server stops accepting new RPCs when the client sends an illegal http2 header.
			continue
		}
		testMalformedHTTP2Metadata(t, e)
	}
}

func testMalformedHTTP2Metadata(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 2718)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: 314,
		Payload:      payload,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), malformedHTTP2Metadata)
	if _, err := tc.UnaryCall(ctx, req); status.Code(err) != codes.Internal {
		t.Fatalf("TestService.UnaryCall(%v, _) = _, %v; want _, %s", ctx, err, codes.Internal)
	}
}

// Tests that the client transparently retries correctly when receiving a
// RST_STREAM with code REFUSED_STREAM.
func (s) TestTransparentRetry(t *testing.T) {
	testCases := []struct {
		failFast bool
		errCode  codes.Code
	}{{
		// success attempt: 1, (stream ID 1)
	}, {
		// success attempt: 2, (stream IDs 3, 5)
	}, {
		// no success attempt (stream IDs 7, 9)
		errCode: codes.Unavailable,
	}, {
		// success attempt: 1 (stream ID 11),
		failFast: true,
	}, {
		// success attempt: 2 (stream IDs 13, 15),
		failFast: true,
	}, {
		// no success attempt (stream IDs 17, 19)
		failFast: true,
		errCode:  codes.Unavailable,
	}}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen. Err: %v", err)
	}
	defer lis.Close()
	server := &httpServer{
		responses: []httpServerResponse{{
			trailers: [][]string{{
				":status", "200",
				"content-type", "application/grpc",
				"grpc-status", "0",
			}},
		}},
		refuseStream: func(i uint32) bool {
			switch i {
			case 1, 5, 11, 15: // these stream IDs succeed
				return false
			}
			return true // these are refused
		},
	}
	server.start(t, lis)
	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial due to err: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := testpb.NewTestServiceClient(cc)

	for i, tc := range testCases {
		stream, err := client.FullDuplexCall(ctx)
		if err != nil {
			t.Fatalf("error creating stream due to err: %v", err)
		}
		code := func(err error) codes.Code {
			if err == io.EOF {
				return codes.OK
			}
			return status.Code(err)
		}
		if _, err := stream.Recv(); code(err) != tc.errCode {
			t.Fatalf("%v: stream.Recv() = _, %v, want error code: %v", i, err, tc.errCode)
		}

	}
}

func (s) TestCancel(t *testing.T) {
	for _, e := range listTestEnv() {
		testCancel(t, e)
	}
}

func testCancel(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise("grpc: the client connection is closing; please retry")
	te.startServer(&testServer{security: e.security, unaryCallSleepTime: time.Second})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	const argSize = 2718
	const respSize = 314

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(1*time.Millisecond, cancel)
	if r, err := tc.UnaryCall(ctx, req); status.Code(err) != codes.Canceled {
		t.Fatalf("TestService/UnaryCall(_, _) = %v, %v; want _, error code: %s", r, err, codes.Canceled)
	}
	awaitNewConnLogOutput()
}

func (s) TestCancelNoIO(t *testing.T) {
	for _, e := range listTestEnv() {
		testCancelNoIO(t, e)
	}
}

func testCancelNoIO(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise("http2Client.notifyError got notified that the client transport was broken")
	te.maxStream = 1 // Only allows 1 live stream per server transport.
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	// Start one blocked RPC for which we'll never send streaming
	// input. This will consume the 1 maximum concurrent streams,
	// causing future RPCs to hang.
	ctx, cancelFirst := context.WithCancel(context.Background())
	_, err := tc.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want _, <nil>", tc, err)
	}

	// Loop until the ClientConn receives the initial settings
	// frame from the server, notifying it about the maximum
	// concurrent streams. We know when it's received it because
	// an RPC will fail with codes.DeadlineExceeded instead of
	// succeeding.
	// TODO(bradfitz): add internal test hook for this (Issue 534)
	for {
		ctx, cancelSecond := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := tc.StreamingInputCall(ctx)
		cancelSecond()
		if err == nil {
			continue
		}
		if status.Code(err) == codes.DeadlineExceeded {
			break
		}
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want _, %s", tc, err, codes.DeadlineExceeded)
	}
	// If there are any RPCs in flight before the client receives
	// the max streams setting, let them be expired.
	// TODO(bradfitz): add internal test hook for this (Issue 534)
	time.Sleep(50 * time.Millisecond)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancelFirst()
	}()

	// This should be blocked until the 1st is canceled, then succeed.
	ctx, cancelThird := context.WithTimeout(context.Background(), 500*time.Millisecond)
	if _, err := tc.StreamingInputCall(ctx); err != nil {
		t.Errorf("%v.StreamingInputCall(_) = _, %v, want _, <nil>", tc, err)
	}
	cancelThird()
}

// The following tests the gRPC streaming RPC implementations.
// TODO(zhaoq): Have better coverage on error cases.
var (
	reqSizes  = []int{27182, 8, 1828, 45904}
	respSizes = []int{31415, 9, 2653, 58979}
)

func (s) TestNoService(t *testing.T) {
	for _, e := range listTestEnv() {
		testNoService(t, e)
	}
}

func testNoService(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(nil)
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	stream, err := tc.FullDuplexCall(te.ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.Unimplemented {
		t.Fatalf("stream.Recv() = _, %v, want _, error code %s", err, codes.Unimplemented)
	}
}

func (s) TestPingPong(t *testing.T) {
	for _, e := range listTestEnv() {
		testPingPong(t, e)
	}
}

func testPingPong(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	var index int
	for index < len(reqSizes) {
		respParam := []*testpb.ResponseParameters{
			{
				Size: int32(respSizes[index]),
			},
		}

		payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(reqSizes[index]))
		if err != nil {
			t.Fatal(err)
		}

		req := &testpb.StreamingOutputCallRequest{
			ResponseType:       testpb.PayloadType_COMPRESSABLE,
			ResponseParameters: respParam,
			Payload:            payload,
		}
		if err := stream.Send(req); err != nil {
			t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
		}
		reply, err := stream.Recv()
		if err != nil {
			t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
		}
		pt := reply.GetPayload().GetType()
		if pt != testpb.PayloadType_COMPRESSABLE {
			t.Fatalf("Got the reply of type %d, want %d", pt, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			t.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() got %v, want %v", stream, err, nil)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v failed to complele the ping pong test: %v", stream, err)
	}
}

func (s) TestMetadataStreamingRPC(t *testing.T) {
	for _, e := range listTestEnv() {
		testMetadataStreamingRPC(t, e)
	}
}

func testMetadataStreamingRPC(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx := metadata.NewOutgoingContext(te.ctx, testMetadata)
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	go func() {
		headerMD, err := stream.Header()
		if e.security == "tls" {
			delete(headerMD, "transport_security_type")
		}
		delete(headerMD, "trailer") // ignore if present
		delete(headerMD, "user-agent")
		delete(headerMD, "content-type")
		if err != nil || !reflect.DeepEqual(testMetadata, headerMD) {
			t.Errorf("#1 %v.Header() = %v, %v, want %v, <nil>", stream, headerMD, err, testMetadata)
		}
		// test the cached value.
		headerMD, err = stream.Header()
		delete(headerMD, "trailer") // ignore if present
		delete(headerMD, "user-agent")
		delete(headerMD, "content-type")
		if err != nil || !reflect.DeepEqual(testMetadata, headerMD) {
			t.Errorf("#2 %v.Header() = %v, %v, want %v, <nil>", stream, headerMD, err, testMetadata)
		}
		err = func() error {
			for index := 0; index < len(reqSizes); index++ {
				respParam := []*testpb.ResponseParameters{
					{
						Size: int32(respSizes[index]),
					},
				}

				payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(reqSizes[index]))
				if err != nil {
					return err
				}

				req := &testpb.StreamingOutputCallRequest{
					ResponseType:       testpb.PayloadType_COMPRESSABLE,
					ResponseParameters: respParam,
					Payload:            payload,
				}
				if err := stream.Send(req); err != nil {
					return fmt.Errorf("%v.Send(%v) = %v, want <nil>", stream, req, err)
				}
			}
			return nil
		}()
		// Tell the server we're done sending args.
		stream.CloseSend()
		if err != nil {
			t.Error(err)
		}
	}()
	for {
		if _, err := stream.Recv(); err != nil {
			break
		}
	}
	trailerMD := stream.Trailer()
	if !reflect.DeepEqual(testTrailerMetadata, trailerMD) {
		t.Fatalf("%v.Trailer() = %v, want %v", stream, trailerMD, testTrailerMetadata)
	}
}

func (s) TestServerStreaming(t *testing.T) {
	for _, e := range listTestEnv() {
		testServerStreaming(t, e)
	}
}

func testServerStreaming(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	respParam := make([]*testpb.ResponseParameters, len(respSizes))
	for i, s := range respSizes {
		respParam[i] = &testpb.ResponseParameters{
			Size: int32(s),
		}
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := tc.StreamingOutputCall(ctx, req)
	if err != nil {
		t.Fatalf("%v.StreamingOutputCall(_) = _, %v, want <nil>", tc, err)
	}
	var rpcStatus error
	var respCnt int
	var index int
	for {
		reply, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
		pt := reply.GetPayload().GetType()
		if pt != testpb.PayloadType_COMPRESSABLE {
			t.Fatalf("Got the reply of type %d, want %d", pt, testpb.PayloadType_COMPRESSABLE)
		}
		size := len(reply.GetPayload().GetBody())
		if size != int(respSizes[index]) {
			t.Fatalf("Got reply body of length %d, want %d", size, respSizes[index])
		}
		index++
		respCnt++
	}
	if rpcStatus != io.EOF {
		t.Fatalf("Failed to finish the server streaming rpc: %v, want <EOF>", rpcStatus)
	}
	if respCnt != len(respSizes) {
		t.Fatalf("Got %d reply, want %d", len(respSizes), respCnt)
	}
}

func (s) TestFailedServerStreaming(t *testing.T) {
	for _, e := range listTestEnv() {
		testFailedServerStreaming(t, e)
	}
}

func testFailedServerStreaming(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = failAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	respParam := make([]*testpb.ResponseParameters, len(respSizes))
	for i, s := range respSizes {
		respParam[i] = &testpb.ResponseParameters{
			Size: int32(s),
		}
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
	}
	ctx := metadata.NewOutgoingContext(te.ctx, testMetadata)
	stream, err := tc.StreamingOutputCall(ctx, req)
	if err != nil {
		t.Fatalf("%v.StreamingOutputCall(_) = _, %v, want <nil>", tc, err)
	}
	wantErr := status.Error(codes.DataLoss, "error for testing: "+failAppUA)
	if _, err := stream.Recv(); !equalError(err, wantErr) {
		t.Fatalf("%v.Recv() = _, %v, want _, %v", stream, err, wantErr)
	}
}

func equalError(x, y error) bool {
	return x == y || (x != nil && y != nil && x.Error() == y.Error())
}

// concurrentSendServer is a TestServiceServer whose
// StreamingOutputCall makes ten serial Send calls, sending payloads
// "0".."9", inclusive.  TestServerStreamingConcurrent verifies they
// were received in the correct order, and that there were no races.
//
// All other TestServiceServer methods crash if called.
type concurrentSendServer struct {
	testpb.TestServiceServer
}

func (s concurrentSendServer) StreamingOutputCall(args *testpb.StreamingOutputCallRequest, stream testpb.TestService_StreamingOutputCallServer) error {
	for i := 0; i < 10; i++ {
		stream.Send(&testpb.StreamingOutputCallResponse{
			Payload: &testpb.Payload{
				Body: []byte{'0' + uint8(i)},
			},
		})
	}
	return nil
}

// Tests doing a bunch of concurrent streaming output calls.
func (s) TestServerStreamingConcurrent(t *testing.T) {
	for _, e := range listTestEnv() {
		testServerStreamingConcurrent(t, e)
	}
}

func testServerStreamingConcurrent(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(concurrentSendServer{})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	doStreamingCall := func() {
		req := &testpb.StreamingOutputCallRequest{}
		ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
		defer cancel()
		stream, err := tc.StreamingOutputCall(ctx, req)
		if err != nil {
			t.Errorf("%v.StreamingOutputCall(_) = _, %v, want <nil>", tc, err)
			return
		}
		var ngot int
		var buf bytes.Buffer
		for {
			reply, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			ngot++
			if buf.Len() > 0 {
				buf.WriteByte(',')
			}
			buf.Write(reply.GetPayload().GetBody())
		}
		if want := 10; ngot != want {
			t.Errorf("Got %d replies, want %d", ngot, want)
		}
		if got, want := buf.String(), "0,1,2,3,4,5,6,7,8,9"; got != want {
			t.Errorf("Got replies %q; want %q", got, want)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doStreamingCall()
		}()
	}
	wg.Wait()

}

func generatePayloadSizes() [][]int {
	reqSizes := [][]int{
		{27182, 8, 1828, 45904},
	}

	num8KPayloads := 1024
	eightKPayloads := []int{}
	for i := 0; i < num8KPayloads; i++ {
		eightKPayloads = append(eightKPayloads, (1 << 13))
	}
	reqSizes = append(reqSizes, eightKPayloads)

	num2MPayloads := 8
	twoMPayloads := []int{}
	for i := 0; i < num2MPayloads; i++ {
		twoMPayloads = append(twoMPayloads, (1 << 21))
	}
	reqSizes = append(reqSizes, twoMPayloads)

	return reqSizes
}

func (s) TestClientStreaming(t *testing.T) {
	for _, s := range generatePayloadSizes() {
		for _, e := range listTestEnv() {
			testClientStreaming(t, e, s)
		}
	}
}

func testClientStreaming(t *testing.T, e env, sizes []int) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	ctx, cancel := context.WithTimeout(te.ctx, time.Second*30)
	defer cancel()
	stream, err := tc.StreamingInputCall(ctx)
	if err != nil {
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want <nil>", tc, err)
	}

	var sum int
	for _, s := range sizes {
		payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(s))
		if err != nil {
			t.Fatal(err)
		}

		req := &testpb.StreamingInputCallRequest{
			Payload: payload,
		}
		if err := stream.Send(req); err != nil {
			t.Fatalf("%v.Send(_) = %v, want <nil>", stream, err)
		}
		sum += s
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("%v.CloseAndRecv() got error %v, want %v", stream, err, nil)
	}
	if reply.GetAggregatedPayloadSize() != int32(sum) {
		t.Fatalf("%v.CloseAndRecv().GetAggregatePayloadSize() = %v; want %v", stream, reply.GetAggregatedPayloadSize(), sum)
	}
}

func (s) TestClientStreamingError(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.name == "handler-tls" {
			continue
		}
		testClientStreamingError(t, e)
	}
}

func testClientStreamingError(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, earlyFail: true})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	stream, err := tc.StreamingInputCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want <nil>", tc, err)
	}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 1)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.StreamingInputCallRequest{
		Payload: payload,
	}
	// The 1st request should go through.
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
	}
	for {
		if err := stream.Send(req); err != io.EOF {
			continue
		}
		if _, err := stream.CloseAndRecv(); status.Code(err) != codes.NotFound {
			t.Fatalf("%v.CloseAndRecv() = %v, want error %s", stream, err, codes.NotFound)
		}
		break
	}
}

func (s) TestExceedMaxStreamsLimit(t *testing.T) {
	for _, e := range listTestEnv() {
		testExceedMaxStreamsLimit(t, e)
	}
}

func testExceedMaxStreamsLimit(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise(
		"http2Client.notifyError got notified that the client transport was broken",
		"Conn.resetTransport failed to create client transport",
		"grpc: the connection is closing",
	)
	te.maxStream = 1 // Only allows 1 live stream per server transport.
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	_, err := tc.StreamingInputCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want _, <nil>", tc, err)
	}
	// Loop until receiving the new max stream setting from the server.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := tc.StreamingInputCall(ctx)
		if err == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if status.Code(err) == codes.DeadlineExceeded {
			break
		}
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want _, %s", tc, err, codes.DeadlineExceeded)
	}
}

func (s) TestStreamsQuotaRecovery(t *testing.T) {
	for _, e := range listTestEnv() {
		testStreamsQuotaRecovery(t, e)
	}
}

func testStreamsQuotaRecovery(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise(
		"http2Client.notifyError got notified that the client transport was broken",
		"Conn.resetTransport failed to create client transport",
		"grpc: the connection is closing",
	)
	te.maxStream = 1 // Allows 1 live stream.
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := tc.StreamingInputCall(ctx); err != nil {
		t.Fatalf("tc.StreamingInputCall(_) = _, %v, want _, <nil>", err)
	}
	// Loop until the new max stream setting is effective.
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := tc.StreamingInputCall(ctx)
		cancel()
		if err == nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if status.Code(err) == codes.DeadlineExceeded {
			break
		}
		t.Fatalf("tc.StreamingInputCall(_) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 314)
			if err != nil {
				t.Error(err)
				return
			}
			req := &testpb.SimpleRequest{
				ResponseType: testpb.PayloadType_COMPRESSABLE,
				ResponseSize: 1592,
				Payload:      payload,
			}
			// No rpc should go through due to the max streams limit.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if _, err := tc.UnaryCall(ctx, req, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
				t.Errorf("tc.UnaryCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
			}
		}()
	}
	wg.Wait()

	cancel()
	// A new stream should be allowed after canceling the first one.
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tc.StreamingInputCall(ctx); err != nil {
		t.Fatalf("tc.StreamingInputCall(_) = _, %v, want _, %v", err, nil)
	}
}

func (s) TestCompressServerHasNoSupport(t *testing.T) {
	for _, e := range listTestEnv() {
		testCompressServerHasNoSupport(t, e)
	}
}

func testCompressServerHasNoSupport(t *testing.T, e env) {
	te := newTest(t, e)
	te.serverCompression = false
	te.clientCompression = false
	te.clientNopCompression = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	const argSize = 271828
	const respSize = 314159
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.Unimplemented {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code %s", err, codes.Unimplemented)
	}
	// Streaming RPC
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.Unimplemented {
		t.Fatalf("%v.Recv() = %v, want error code %s", stream, err, codes.Unimplemented)
	}
}

func (s) TestCompressOK(t *testing.T) {
	for _, e := range listTestEnv() {
		testCompressOK(t, e)
	}
}

func testCompressOK(t *testing.T, e env) {
	te := newTest(t, e)
	te.serverCompression = true
	te.clientCompression = true
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	// Unary call
	const argSize = 271828
	const respSize = 314159
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("something", "something"))
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, <nil>", err)
	}
	// Streaming RPC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam := []*testpb.ResponseParameters{
		{
			Size: 31415,
		},
	}
	payload, err = newPayload(testpb.PayloadType_COMPRESSABLE, int32(31415))
	if err != nil {
		t.Fatal(err)
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	stream.CloseSend()
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v.Recv() = %v, want io.EOF", stream, err)
	}
}

func (s) TestIdentityEncoding(t *testing.T) {
	for _, e := range listTestEnv() {
		testIdentityEncoding(t, e)
	}
}

func testIdentityEncoding(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	// Unary call
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 5)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: 10,
		Payload:      payload,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("something", "something"))
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, <nil>", err)
	}
	// Streaming RPC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx, grpc.UseCompressor("identity"))
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	payload, err = newPayload(testpb.PayloadType_COMPRESSABLE, int32(31415))
	if err != nil {
		t.Fatal(err)
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: []*testpb.ResponseParameters{{Size: 10}},
		Payload:            payload,
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	stream.CloseSend()
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("%v.Recv() = %v, want io.EOF", stream, err)
	}
}

func (s) TestUnaryClientInterceptor(t *testing.T) {
	for _, e := range listTestEnv() {
		testUnaryClientInterceptor(t, e)
	}
}

func failOkayRPC(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	err := invoker(ctx, method, req, reply, cc, opts...)
	if err == nil {
		return status.Error(codes.NotFound, "")
	}
	return err
}

func testUnaryClientInterceptor(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.unaryClientInt = failOkayRPC
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	tc := testpb.NewTestServiceClient(te.clientConn())
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.NotFound {
		t.Fatalf("%v.EmptyCall(_, _) = _, %v, want _, error code %s", tc, err, codes.NotFound)
	}
}

func (s) TestStreamClientInterceptor(t *testing.T) {
	for _, e := range listTestEnv() {
		testStreamClientInterceptor(t, e)
	}
}

func failOkayStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	s, err := streamer(ctx, desc, cc, method, opts...)
	if err == nil {
		return nil, status.Error(codes.NotFound, "")
	}
	return s, nil
}

func testStreamClientInterceptor(t *testing.T, e env) {
	te := newTest(t, e)
	te.streamClientInt = failOkayStream
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	tc := testpb.NewTestServiceClient(te.clientConn())
	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(1),
		},
	}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(1))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.StreamingOutputCall(ctx, req); status.Code(err) != codes.NotFound {
		t.Fatalf("%v.StreamingOutputCall(_) = _, %v, want _, error code %s", tc, err, codes.NotFound)
	}
}

func (s) TestUnaryServerInterceptor(t *testing.T) {
	for _, e := range listTestEnv() {
		testUnaryServerInterceptor(t, e)
	}
}

func errInjector(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return nil, status.Error(codes.PermissionDenied, "")
}

func testUnaryServerInterceptor(t *testing.T, e env) {
	te := newTest(t, e)
	te.unaryServerInt = errInjector
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	tc := testpb.NewTestServiceClient(te.clientConn())
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("%v.EmptyCall(_, _) = _, %v, want _, error code %s", tc, err, codes.PermissionDenied)
	}
}

func (s) TestStreamServerInterceptor(t *testing.T) {
	for _, e := range listTestEnv() {
		// TODO(bradfitz): Temporarily skip this env due to #619.
		if e.name == "handler-tls" {
			continue
		}
		testStreamServerInterceptor(t, e)
	}
}

func fullDuplexOnly(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if info.FullMethod == "/grpc.testing.TestService/FullDuplexCall" {
		return handler(srv, ss)
	}
	// Reject the other methods.
	return status.Error(codes.PermissionDenied, "")
}

func testStreamServerInterceptor(t *testing.T, e env) {
	te := newTest(t, e)
	te.streamServerInt = fullDuplexOnly
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	tc := testpb.NewTestServiceClient(te.clientConn())
	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(1),
		},
	}
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(1))
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	s1, err := tc.StreamingOutputCall(ctx, req)
	if err != nil {
		t.Fatalf("%v.StreamingOutputCall(_) = _, %v, want _, <nil>", tc, err)
	}
	if _, err := s1.Recv(); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("%v.StreamingInputCall(_) = _, %v, want _, error code %s", tc, err, codes.PermissionDenied)
	}
	s2, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := s2.Send(req); err != nil {
		t.Fatalf("%v.Send(_) = %v, want <nil>", s2, err)
	}
	if _, err := s2.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want _, <nil>", s2, err)
	}
}

// funcServer implements methods of TestServiceServer using funcs,
// similar to an http.HandlerFunc.
// Any unimplemented method will crash. Tests implement the method(s)
// they need.
type funcServer struct {
	testpb.TestServiceServer
	unaryCall          func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error)
	streamingInputCall func(stream testpb.TestService_StreamingInputCallServer) error
	fullDuplexCall     func(stream testpb.TestService_FullDuplexCallServer) error
}

func (s *funcServer) UnaryCall(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return s.unaryCall(ctx, in)
}

func (s *funcServer) StreamingInputCall(stream testpb.TestService_StreamingInputCallServer) error {
	return s.streamingInputCall(stream)
}

func (s *funcServer) FullDuplexCall(stream testpb.TestService_FullDuplexCallServer) error {
	return s.fullDuplexCall(stream)
}

func (s) TestClientRequestBodyErrorUnexpectedEOF(t *testing.T) {
	for _, e := range listTestEnv() {
		testClientRequestBodyErrorUnexpectedEOF(t, e)
	}
}

func testClientRequestBodyErrorUnexpectedEOF(t *testing.T, e env) {
	te := newTest(t, e)
	ts := &funcServer{unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
		errUnexpectedCall := errors.New("unexpected call func server method")
		t.Error(errUnexpectedCall)
		return nil, errUnexpectedCall
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/UnaryCall", false)
		// Say we have 5 bytes coming, but set END_STREAM flag:
		st.writeData(1, true, []byte{0, 0, 0, 0, 5})
		st.wantAnyFrame() // wait for server to crash (it used to crash)
	})
}

func (s) TestClientRequestBodyErrorCloseAfterLength(t *testing.T) {
	for _, e := range listTestEnv() {
		testClientRequestBodyErrorCloseAfterLength(t, e)
	}
}

func testClientRequestBodyErrorCloseAfterLength(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise("Server.processUnaryRPC failed to write status")
	ts := &funcServer{unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
		errUnexpectedCall := errors.New("unexpected call func server method")
		t.Error(errUnexpectedCall)
		return nil, errUnexpectedCall
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/UnaryCall", false)
		// say we're sending 5 bytes, but then close the connection instead.
		st.writeData(1, false, []byte{0, 0, 0, 0, 5})
		st.cc.Close()
	})
}

func (s) TestClientRequestBodyErrorCancel(t *testing.T) {
	for _, e := range listTestEnv() {
		testClientRequestBodyErrorCancel(t, e)
	}
}

func testClientRequestBodyErrorCancel(t *testing.T, e env) {
	te := newTest(t, e)
	gotCall := make(chan bool, 1)
	ts := &funcServer{unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
		gotCall <- true
		return new(testpb.SimpleResponse), nil
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/UnaryCall", false)
		// Say we have 5 bytes coming, but cancel it instead.
		st.writeRSTStream(1, http2.ErrCodeCancel)
		st.writeData(1, false, []byte{0, 0, 0, 0, 5})

		// Verify we didn't a call yet.
		select {
		case <-gotCall:
			t.Fatal("unexpected call")
		default:
		}

		// And now send an uncanceled (but still invalid), just to get a response.
		st.writeHeadersGRPC(3, "/grpc.testing.TestService/UnaryCall", false)
		st.writeData(3, true, []byte{0, 0, 0, 0, 0})
		<-gotCall
		st.wantAnyFrame()
	})
}

func (s) TestClientRequestBodyErrorCancelStreamingInput(t *testing.T) {
	for _, e := range listTestEnv() {
		testClientRequestBodyErrorCancelStreamingInput(t, e)
	}
}

func testClientRequestBodyErrorCancelStreamingInput(t *testing.T, e env) {
	te := newTest(t, e)
	recvErr := make(chan error, 1)
	ts := &funcServer{streamingInputCall: func(stream testpb.TestService_StreamingInputCallServer) error {
		_, err := stream.Recv()
		recvErr <- err
		return nil
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/StreamingInputCall", false)
		// Say we have 5 bytes coming, but cancel it instead.
		st.writeData(1, false, []byte{0, 0, 0, 0, 5})
		st.writeRSTStream(1, http2.ErrCodeCancel)

		var got error
		select {
		case got = <-recvErr:
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for error")
		}
		if grpc.Code(got) != codes.Canceled {
			t.Errorf("error = %#v; want error code %s", got, codes.Canceled)
		}
	})
}

func (s) TestClientInitialHeaderEndStream(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			continue
		}
		testClientInitialHeaderEndStream(t, e)
	}
}

func testClientInitialHeaderEndStream(t *testing.T, e env) {
	// To ensure RST_STREAM is sent for illegal data write and not normal stream
	// close.
	frameCheckingDone := make(chan struct{})
	// To ensure goroutine for test does not end before RPC handler performs error
	// checking.
	handlerDone := make(chan struct{})
	te := newTest(t, e)
	ts := &funcServer{streamingInputCall: func(stream testpb.TestService_StreamingInputCallServer) error {
		defer close(handlerDone)
		// Block on serverTester receiving RST_STREAM. This ensures server has closed
		// stream before stream.Recv().
		<-frameCheckingDone
		data, err := stream.Recv()
		if err == nil {
			t.Errorf("unexpected data received in func server method: '%v'", data)
		} else if status.Code(err) != codes.Canceled {
			t.Errorf("expected canceled error, instead received '%v'", err)
		}
		return nil
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		// Send a headers with END_STREAM flag, but then write data.
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/StreamingInputCall", true)
		st.writeData(1, false, []byte{0, 0, 0, 0, 0})
		st.wantAnyFrame()
		st.wantAnyFrame()
		st.wantRSTStream(http2.ErrCodeStreamClosed)
		close(frameCheckingDone)
		<-handlerDone
	})
}

func (s) TestClientSendDataAfterCloseSend(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			continue
		}
		testClientSendDataAfterCloseSend(t, e)
	}
}

func testClientSendDataAfterCloseSend(t *testing.T, e env) {
	// To ensure RST_STREAM is sent for illegal data write prior to execution of RPC
	// handler.
	frameCheckingDone := make(chan struct{})
	// To ensure goroutine for test does not end before RPC handler performs error
	// checking.
	handlerDone := make(chan struct{})
	te := newTest(t, e)
	ts := &funcServer{streamingInputCall: func(stream testpb.TestService_StreamingInputCallServer) error {
		defer close(handlerDone)
		// Block on serverTester receiving RST_STREAM. This ensures server has closed
		// stream before stream.Recv().
		<-frameCheckingDone
		for {
			_, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				if status.Code(err) != codes.Canceled {
					t.Errorf("expected canceled error, instead received '%v'", err)
				}
				break
			}
		}
		if err := stream.SendMsg(nil); err == nil {
			t.Error("expected error sending message on stream after stream closed due to illegal data")
		} else if status.Code(err) != codes.Canceled {
			t.Errorf("expected cancel error, instead received '%v'", err)
		}
		return nil
	}}
	te.startServer(ts)
	defer te.tearDown()
	te.withServerTester(func(st *serverTester) {
		st.writeHeadersGRPC(1, "/grpc.testing.TestService/StreamingInputCall", false)
		// Send data with END_STREAM flag, but then write more data.
		st.writeData(1, true, []byte{0, 0, 0, 0, 0})
		st.writeData(1, false, []byte{0, 0, 0, 0, 0})
		st.wantAnyFrame()
		st.wantAnyFrame()
		st.wantRSTStream(http2.ErrCodeStreamClosed)
		close(frameCheckingDone)
		<-handlerDone
	})
}

func (s) TestClientResourceExhaustedCancelFullDuplex(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			// httpHandler write won't be blocked on flow control window.
			continue
		}
		testClientResourceExhaustedCancelFullDuplex(t, e)
	}
}

func testClientResourceExhaustedCancelFullDuplex(t *testing.T, e env) {
	te := newTest(t, e)
	recvErr := make(chan error, 1)
	ts := &funcServer{fullDuplexCall: func(stream testpb.TestService_FullDuplexCallServer) error {
		defer close(recvErr)
		_, err := stream.Recv()
		if err != nil {
			return status.Errorf(codes.Internal, "stream.Recv() got error: %v, want <nil>", err)
		}
		// create a payload that's larger than the default flow control window.
		payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 10)
		if err != nil {
			return err
		}
		resp := &testpb.StreamingOutputCallResponse{
			Payload: payload,
		}
		ce := make(chan error, 1)
		go func() {
			var err error
			for {
				if err = stream.Send(resp); err != nil {
					break
				}
			}
			ce <- err
		}()
		select {
		case err = <-ce:
		case <-time.After(10 * time.Second):
			err = errors.New("10s timeout reached")
		}
		recvErr <- err
		return err
	}}
	te.startServer(ts)
	defer te.tearDown()
	// set a low limit on receive message size to error with Resource Exhausted on
	// client side when server send a large message.
	te.maxClientReceiveMsgSize = newInt(10)
	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	req := &testpb.StreamingOutputCallRequest{}
	if err := stream.Send(req); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
	err = <-recvErr
	if status.Code(err) != codes.Canceled {
		t.Fatalf("server got error %v, want error code: %s", err, codes.Canceled)
	}
}

type clientFailCreds struct{}

func (c *clientFailCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, nil, nil
}
func (c *clientFailCreds) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, fmt.Errorf("client handshake fails with fatal error")
}
func (c *clientFailCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}
func (c *clientFailCreds) Clone() credentials.TransportCredentials {
	return c
}
func (c *clientFailCreds) OverrideServerName(s string) error {
	return nil
}

// This test makes sure that failfast RPCs fail if client handshake fails with
// fatal errors.
func (s) TestFailfastRPCFailOnFatalHandshakeError(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()

	cc, err := grpc.Dial("passthrough:///"+lis.Addr().String(), grpc.WithTransportCredentials(&clientFailCreds{}))
	if err != nil {
		t.Fatalf("grpc.Dial(_) = %v", err)
	}
	defer cc.Close()

	tc := testpb.NewTestServiceClient(cc)
	// This unary call should fail, but not timeout.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(false)); status.Code(err) != codes.Unavailable {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want <Unavailable>", err)
	}
}

func (s) TestFlowControlLogicalRace(t *testing.T) {
	// Test for a regression of https://github.com/grpc/grpc-go/issues/632,
	// and other flow control bugs.

	const (
		itemCount   = 100
		itemSize    = 1 << 10
		recvCount   = 2
		maxFailures = 3

		requestTimeout = time.Second * 5
	)

	requestCount := 10000
	if raceMode {
		requestCount = 1000
	}

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer lis.Close()

	s := grpc.NewServer()
	testpb.RegisterTestServiceServer(s, &flowControlLogicalRaceServer{
		itemCount: itemCount,
		itemSize:  itemSize,
	})
	defer s.Stop()

	go s.Serve(lis)

	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.Dial(%q) = %v", lis.Addr().String(), err)
	}
	defer cc.Close()
	cl := testpb.NewTestServiceClient(cc)

	failures := 0
	for i := 0; i < requestCount; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		output, err := cl.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
		if err != nil {
			t.Fatalf("StreamingOutputCall; err = %q", err)
		}

		j := 0
	loop:
		for ; j < recvCount; j++ {
			_, err := output.Recv()
			if err != nil {
				if err == io.EOF {
					break loop
				}
				switch status.Code(err) {
				case codes.DeadlineExceeded:
					break loop
				default:
					t.Fatalf("Recv; err = %q", err)
				}
			}
		}
		cancel()
		<-ctx.Done()

		if j < recvCount {
			t.Errorf("got %d responses to request %d", j, i)
			failures++
			if failures >= maxFailures {
				// Continue past the first failure to see if the connection is
				// entirely broken, or if only a single RPC was affected
				break
			}
		}
	}
}

type flowControlLogicalRaceServer struct {
	testpb.TestServiceServer

	itemSize  int
	itemCount int
}

func (s *flowControlLogicalRaceServer) StreamingOutputCall(req *testpb.StreamingOutputCallRequest, srv testpb.TestService_StreamingOutputCallServer) error {
	for i := 0; i < s.itemCount; i++ {
		err := srv.Send(&testpb.StreamingOutputCallResponse{
			Payload: &testpb.Payload{
				// Sending a large stream of data which the client reject
				// helps to trigger some types of flow control bugs.
				//
				// Reallocating memory here is inefficient, but the stress it
				// puts on the GC leads to more frequent flow control
				// failures. The GC likely causes more variety in the
				// goroutine scheduling orders.
				Body: bytes.Repeat([]byte("a"), s.itemSize),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type lockingWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (lw *lockingWriter) Write(p []byte) (n int, err error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}

func (lw *lockingWriter) setWriter(w io.Writer) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.w = w
}

var testLogOutput = &lockingWriter{w: os.Stderr}

// awaitNewConnLogOutput waits for any of grpc.NewConn's goroutines to
// terminate, if they're still running. It spams logs with this
// message.  We wait for it so our log filter is still
// active. Otherwise the "defer restore()" at the top of various test
// functions restores our log filter and then the goroutine spams.
func awaitNewConnLogOutput() {
	awaitLogOutput(50*time.Millisecond, "grpc: the client connection is closing; please retry")
}

func awaitLogOutput(maxWait time.Duration, phrase string) {
	pb := []byte(phrase)

	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	wakeup := make(chan bool, 1)
	for {
		if logOutputHasContents(pb, wakeup) {
			return
		}
		select {
		case <-timer.C:
			// Too slow. Oh well.
			return
		case <-wakeup:
		}
	}
}

func logOutputHasContents(v []byte, wakeup chan<- bool) bool {
	testLogOutput.mu.Lock()
	defer testLogOutput.mu.Unlock()
	fw, ok := testLogOutput.w.(*filterWriter)
	if !ok {
		return false
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if bytes.Contains(fw.buf.Bytes(), v) {
		return true
	}
	fw.wakeup = wakeup
	return false
}

var verboseLogs = flag.Bool("verbose_logs", false, "show all log output, without filtering")

func noop() {}

// declareLogNoise declares that t is expected to emit the following noisy
// phrases, even on success. Those phrases will be filtered from log output and
// only be shown if *verbose_logs or t ends up failing. The returned restore
// function should be called with defer to be run before the test ends.
func declareLogNoise(t *testing.T, phrases ...string) (restore func()) {
	if *verboseLogs {
		return noop
	}
	fw := &filterWriter{dst: os.Stderr, filter: phrases}
	testLogOutput.setWriter(fw)
	return func() {
		if t.Failed() {
			fw.mu.Lock()
			defer fw.mu.Unlock()
			if fw.buf.Len() > 0 {
				t.Logf("Complete log output:\n%s", fw.buf.Bytes())
			}
		}
		testLogOutput.setWriter(os.Stderr)
	}
}

type filterWriter struct {
	dst    io.Writer
	filter []string

	mu     sync.Mutex
	buf    bytes.Buffer
	wakeup chan<- bool // if non-nil, gets true on write
}

func (fw *filterWriter) Write(p []byte) (n int, err error) {
	fw.mu.Lock()
	fw.buf.Write(p)
	if fw.wakeup != nil {
		select {
		case fw.wakeup <- true:
		default:
		}
	}
	fw.mu.Unlock()

	ps := string(p)
	for _, f := range fw.filter {
		if strings.Contains(ps, f) {
			return len(p), nil
		}
	}
	return fw.dst.Write(p)
}

func (s) TestGRPCMethod(t *testing.T) {
	var method string
	var ok bool

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			method, ok = grpc.Method(ctx)
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	if want := "/grpc.testing.TestService/EmptyCall"; !ok || method != want {
		t.Fatalf("grpc.Method(_) = %q, %v; want %q, true", method, ok, want)
	}
}

// renameProtoCodec is an encoding.Codec wrapper that allows customizing the
// Name() of another codec.
type renameProtoCodec struct {
	encoding.Codec
	name string
}

func (r *renameProtoCodec) Name() string { return r.name }

// TestForceCodecName confirms that the ForceCodec call option sets the subtype
// in the content-type header according to the Name() of the codec provided.
func (s) TestForceCodecName(t *testing.T) {
	wantContentTypeCh := make(chan []string, 1)
	defer close(wantContentTypeCh)

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			md, ok := metadata.FromIncomingContext(ctx)
			if !ok {
				return nil, status.Errorf(codes.Internal, "no metadata in context")
			}
			if got, want := md["content-type"], <-wantContentTypeCh; !reflect.DeepEqual(got, want) {
				return nil, status.Errorf(codes.Internal, "got content-type=%q; want [%q]", got, want)
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{grpc.ForceServerCodec(encoding.GetCodec("proto"))}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	codec := &renameProtoCodec{Codec: encoding.GetCodec("proto"), name: "some-test-name"}
	wantContentTypeCh <- []string{"application/grpc+some-test-name"}
	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.ForceCodec(codec)); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	// Confirm the name is converted to lowercase before transmitting.
	codec.name = "aNoTHeRNaME"
	wantContentTypeCh <- []string{"application/grpc+anothername"}
	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}, grpc.ForceCodec(codec)); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}
}

func (s) TestForceServerCodec(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}
	codec := &countingProtoCodec{}
	if err := ss.Start([]grpc.ServerOption{grpc.ForceServerCodec(codec)}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v; want _, nil", err)
	}

	unmarshalCount := atomic.LoadInt32(&codec.unmarshalCount)
	const wantUnmarshalCount = 1
	if unmarshalCount != wantUnmarshalCount {
		t.Fatalf("protoCodec.unmarshalCount = %d; want %d", unmarshalCount, wantUnmarshalCount)
	}
	marshalCount := atomic.LoadInt32(&codec.marshalCount)
	const wantMarshalCount = 1
	if marshalCount != wantMarshalCount {
		t.Fatalf("protoCodec.marshalCount = %d; want %d", marshalCount, wantMarshalCount)
	}
}

func (s) TestUnaryProxyDoesNotForwardMetadata(t *testing.T) {
	const mdkey = "somedata"

	// endpoint ensures mdkey is NOT in metadata and returns an error if it is.
	endpoint := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			if md, ok := metadata.FromIncomingContext(ctx); !ok || md[mdkey] != nil {
				return nil, status.Errorf(codes.Internal, "endpoint: md=%v; want !contains(%q)", md, mdkey)
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := endpoint.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer endpoint.Stop()

	// proxy ensures mdkey IS in metadata, then forwards the RPC to endpoint
	// without explicitly copying the metadata.
	proxy := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			if md, ok := metadata.FromIncomingContext(ctx); !ok || md[mdkey] == nil {
				return nil, status.Errorf(codes.Internal, "proxy: md=%v; want contains(%q)", md, mdkey)
			}
			return endpoint.Client.EmptyCall(ctx, in)
		},
	}
	if err := proxy.Start(nil); err != nil {
		t.Fatalf("Error starting proxy server: %v", err)
	}
	defer proxy.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	md := metadata.Pairs(mdkey, "val")
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Sanity check that endpoint properly errors when it sees mdkey.
	_, err := endpoint.Client.EmptyCall(ctx, &testpb.Empty{})
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Internal {
		t.Fatalf("endpoint.Client.EmptyCall(_, _) = _, %v; want _, <status with Code()=Internal>", err)
	}

	if _, err := proxy.Client.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatal(err.Error())
	}
}

func (s) TestStreamingProxyDoesNotForwardMetadata(t *testing.T) {
	const mdkey = "somedata"

	// doFDC performs a FullDuplexCall with client and returns the error from the
	// first stream.Recv call, or nil if that error is io.EOF.  Calls t.Fatal if
	// the stream cannot be established.
	doFDC := func(ctx context.Context, client testpb.TestServiceClient) error {
		stream, err := client.FullDuplexCall(ctx)
		if err != nil {
			t.Fatalf("Unwanted error: %v", err)
		}
		if _, err := stream.Recv(); err != io.EOF {
			return err
		}
		return nil
	}

	// endpoint ensures mdkey is NOT in metadata and returns an error if it is.
	endpoint := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			ctx := stream.Context()
			if md, ok := metadata.FromIncomingContext(ctx); !ok || md[mdkey] != nil {
				return status.Errorf(codes.Internal, "endpoint: md=%v; want !contains(%q)", md, mdkey)
			}
			return nil
		},
	}
	if err := endpoint.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer endpoint.Stop()

	// proxy ensures mdkey IS in metadata, then forwards the RPC to endpoint
	// without explicitly copying the metadata.
	proxy := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			ctx := stream.Context()
			if md, ok := metadata.FromIncomingContext(ctx); !ok || md[mdkey] == nil {
				return status.Errorf(codes.Internal, "endpoint: md=%v; want !contains(%q)", md, mdkey)
			}
			return doFDC(ctx, endpoint.Client)
		},
	}
	if err := proxy.Start(nil); err != nil {
		t.Fatalf("Error starting proxy server: %v", err)
	}
	defer proxy.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	md := metadata.Pairs(mdkey, "val")
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Sanity check that endpoint properly errors when it sees mdkey in ctx.
	err := doFDC(ctx, endpoint.Client)
	if s, ok := status.FromError(err); !ok || s.Code() != codes.Internal {
		t.Fatalf("stream.Recv() = _, %v; want _, <status with Code()=Internal>", err)
	}

	if err := doFDC(ctx, proxy.Client); err != nil {
		t.Fatalf("doFDC(_, proxy.Client) = %v; want nil", err)
	}
}

func (s) TestStatsTagsAndTrace(t *testing.T) {
	// Data added to context by client (typically in a stats handler).
	tags := []byte{1, 5, 2, 4, 3}
	trace := []byte{5, 2, 1, 3, 4}

	// endpoint ensures Tags() and Trace() in context match those that were added
	// by the client and returns an error if not.
	endpoint := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			md, _ := metadata.FromIncomingContext(ctx)
			if tg := stats.Tags(ctx); !reflect.DeepEqual(tg, tags) {
				return nil, status.Errorf(codes.Internal, "stats.Tags(%v)=%v; want %v", ctx, tg, tags)
			}
			if !reflect.DeepEqual(md["grpc-tags-bin"], []string{string(tags)}) {
				return nil, status.Errorf(codes.Internal, "md['grpc-tags-bin']=%v; want %v", md["grpc-tags-bin"], tags)
			}
			if tr := stats.Trace(ctx); !reflect.DeepEqual(tr, trace) {
				return nil, status.Errorf(codes.Internal, "stats.Trace(%v)=%v; want %v", ctx, tr, trace)
			}
			if !reflect.DeepEqual(md["grpc-trace-bin"], []string{string(trace)}) {
				return nil, status.Errorf(codes.Internal, "md['grpc-trace-bin']=%v; want %v", md["grpc-trace-bin"], trace)
			}
			return &testpb.Empty{}, nil
		},
	}
	if err := endpoint.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer endpoint.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	testCases := []struct {
		ctx  context.Context
		want codes.Code
	}{
		{ctx: ctx, want: codes.Internal},
		{ctx: stats.SetTags(ctx, tags), want: codes.Internal},
		{ctx: stats.SetTrace(ctx, trace), want: codes.Internal},
		{ctx: stats.SetTags(stats.SetTrace(ctx, tags), tags), want: codes.Internal},
		{ctx: stats.SetTags(stats.SetTrace(ctx, trace), tags), want: codes.OK},
	}

	for _, tc := range testCases {
		_, err := endpoint.Client.EmptyCall(tc.ctx, &testpb.Empty{})
		if tc.want == codes.OK && err != nil {
			t.Fatalf("endpoint.Client.EmptyCall(%v, _) = _, %v; want _, nil", tc.ctx, err)
		}
		if s, ok := status.FromError(err); !ok || s.Code() != tc.want {
			t.Fatalf("endpoint.Client.EmptyCall(%v, _) = _, %v; want _, <status with Code()=%v>", tc.ctx, err, tc.want)
		}
	}
}

func (s) TestTapTimeout(t *testing.T) {
	sopts := []grpc.ServerOption{
		grpc.InTapHandle(func(ctx context.Context, _ *tap.Info) (context.Context, error) {
			c, cancel := context.WithCancel(ctx)
			// Call cancel instead of setting a deadline so we can detect which error
			// occurred -- this cancellation (desired) or the client's deadline
			// expired (indicating this cancellation did not affect the RPC).
			time.AfterFunc(10*time.Millisecond, cancel)
			return c, nil
		}),
	}

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			<-ctx.Done()
			return nil, status.Errorf(codes.Canceled, ctx.Err().Error())
		},
	}
	if err := ss.Start(sopts); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	// This was known to be flaky; test several times.
	for i := 0; i < 10; i++ {
		// Set our own deadline in case the server hangs.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		res, err := ss.Client.EmptyCall(ctx, &testpb.Empty{})
		cancel()
		if s, ok := status.FromError(err); !ok || s.Code() != codes.Canceled {
			t.Fatalf("ss.Client.EmptyCall(ctx, _) = %v, %v; want nil, <status with Code()=Canceled>", res, err)
		}
	}

}

func (s) TestClientWriteFailsAfterServerClosesStream(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			return status.Errorf(codes.Internal, "")
		},
	}
	sopts := []grpc.ServerOption{}
	if err := ss.Start(sopts); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := ss.Client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("Error while creating stream: %v", err)
	}
	for {
		if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err == nil {
			time.Sleep(5 * time.Millisecond)
		} else if err == io.EOF {
			break // Success.
		} else {
			t.Fatalf("stream.Send(_) = %v, want io.EOF", err)
		}
	}
}

type windowSizeConfig struct {
	serverStream int32
	serverConn   int32
	clientStream int32
	clientConn   int32
}

func max(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func (s) TestConfigurableWindowSizeWithLargeWindow(t *testing.T) {
	wc := windowSizeConfig{
		serverStream: 8 * 1024 * 1024,
		serverConn:   12 * 1024 * 1024,
		clientStream: 6 * 1024 * 1024,
		clientConn:   8 * 1024 * 1024,
	}
	for _, e := range listTestEnv() {
		testConfigurableWindowSize(t, e, wc)
	}
}

func (s) TestConfigurableWindowSizeWithSmallWindow(t *testing.T) {
	wc := windowSizeConfig{
		serverStream: 1,
		serverConn:   1,
		clientStream: 1,
		clientConn:   1,
	}
	for _, e := range listTestEnv() {
		testConfigurableWindowSize(t, e, wc)
	}
}

func testConfigurableWindowSize(t *testing.T, e env, wc windowSizeConfig) {
	te := newTest(t, e)
	te.serverInitialWindowSize = wc.serverStream
	te.serverInitialConnWindowSize = wc.serverConn
	te.clientInitialWindowSize = wc.clientStream
	te.clientInitialConnWindowSize = wc.clientConn

	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	numOfIter := 11
	// Set message size to exhaust largest of window sizes.
	messageSize := max(max(wc.serverStream, wc.serverConn), max(wc.clientStream, wc.clientConn)) / int32(numOfIter-1)
	messageSize = max(messageSize, 64*1024)
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, messageSize)
	if err != nil {
		t.Fatal(err)
	}
	respParams := []*testpb.ResponseParameters{
		{
			Size: messageSize,
		},
	}
	req := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParams,
		Payload:            payload,
	}
	for i := 0; i < numOfIter; i++ {
		if err := stream.Send(req); err != nil {
			t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, req, err)
		}
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("%v.Recv() = _, %v, want _, <nil>", stream, err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("%v.CloseSend() = %v, want <nil>", stream, err)
	}
}

func (s) TestWaitForReadyConnection(t *testing.T) {
	for _, e := range listTestEnv() {
		testWaitForReadyConnection(t, e)
	}

}

func testWaitForReadyConnection(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn() // Non-blocking dial.
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	state := cc.GetState()
	// Wait for connection to be Ready.
	for ; state != connectivity.Ready && cc.WaitForStateChange(ctx, state); state = cc.GetState() {
	}
	if state != connectivity.Ready {
		t.Fatalf("Want connection state to be Ready, got %v", state)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Make a fail-fast RPC.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("TestService/EmptyCall(_,_) = _, %v, want _, nil", err)
	}
}

type errCodec struct {
	noError bool
}

func (c *errCodec) Marshal(v interface{}) ([]byte, error) {
	if c.noError {
		return []byte{}, nil
	}
	return nil, fmt.Errorf("3987^12 + 4365^12 = 4472^12")
}

func (c *errCodec) Unmarshal(data []byte, v interface{}) error {
	return nil
}

func (c *errCodec) Name() string {
	return "Fermat's near-miss."
}

type countingProtoCodec struct {
	marshalCount   int32
	unmarshalCount int32
}

func (p *countingProtoCodec) Marshal(v interface{}) ([]byte, error) {
	atomic.AddInt32(&p.marshalCount, 1)
	vv, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to marshal, message is %T, want proto.Message", v)
	}
	return proto.Marshal(vv)
}

func (p *countingProtoCodec) Unmarshal(data []byte, v interface{}) error {
	atomic.AddInt32(&p.unmarshalCount, 1)
	vv, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("failed to unmarshal, message is %T, want proto.Message", v)
	}
	return proto.Unmarshal(data, vv)
}

func (*countingProtoCodec) Name() string {
	return "proto"
}

func (s) TestEncodeDoesntPanic(t *testing.T) {
	for _, e := range listTestEnv() {
		testEncodeDoesntPanic(t, e)
	}
}

func testEncodeDoesntPanic(t *testing.T, e env) {
	te := newTest(t, e)
	erc := &errCodec{}
	te.customCodec = erc
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	te.customCodec = nil
	tc := testpb.NewTestServiceClient(te.clientConn())
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Failure case, should not panic.
	tc.EmptyCall(ctx, &testpb.Empty{})
	erc.noError = true
	// Passing case.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("EmptyCall(_, _) = _, %v, want _, <nil>", err)
	}
}

func (s) TestSvrWriteStatusEarlyWrite(t *testing.T) {
	for _, e := range listTestEnv() {
		testSvrWriteStatusEarlyWrite(t, e)
	}
}

func testSvrWriteStatusEarlyWrite(t *testing.T, e env) {
	te := newTest(t, e)
	const smallSize = 1024
	const largeSize = 2048
	const extraLargeSize = 4096
	te.maxServerReceiveMsgSize = newInt(largeSize)
	te.maxServerSendMsgSize = newInt(largeSize)
	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}
	extraLargePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, extraLargeSize)
	if err != nil {
		t.Fatal(err)
	}
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())
	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(smallSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            extraLargePayload,
	}
	// Test recv case: server receives a message larger than maxServerReceiveMsgSize.
	stream, err := tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send() = _, %v, want <nil>", stream, err)
	}
	if _, err = stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
	// Test send case: server sends a message larger than maxServerSendMsgSize.
	sreq.Payload = smallPayload
	respParam[0].Size = int32(extraLargeSize)

	stream, err = tc.FullDuplexCall(te.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err = stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err = stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}
}

// The following functions with function name ending with TD indicates that they
// should be deleted after old service config API is deprecated and deleted.
func testServiceConfigSetupTD(t *testing.T, e env) (*test, chan grpc.ServiceConfig) {
	te := newTest(t, e)
	// We write before read.
	ch := make(chan grpc.ServiceConfig, 1)
	te.sc = ch
	te.userAgent = testAppUA
	te.declareLogNoise(
		"transport: http2Client.notifyError got notified that the client transport was broken EOF",
		"grpc: addrConn.transportMonitor exits due to: grpc: the connection is closing",
		"grpc: addrConn.resetTransport failed to create client transport: connection error",
		"Failed to dial : context canceled; please retry.",
	)
	return te, ch
}

func (s) TestServiceConfigGetMethodConfigTD(t *testing.T) {
	for _, e := range listTestEnv() {
		testGetMethodConfigTD(t, e)
	}
}

func testGetMethodConfigTD(t *testing.T, e env) {
	te, ch := testServiceConfigSetupTD(t, e)
	defer te.tearDown()

	mc1 := grpc.MethodConfig{
		WaitForReady: newBool(true),
		Timeout:      newDuration(time.Millisecond),
	}
	mc2 := grpc.MethodConfig{WaitForReady: newBool(false)}
	m := make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/EmptyCall"] = mc1
	m["/grpc.testing.TestService/"] = mc2
	sc := grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}

	m = make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/UnaryCall"] = mc1
	m["/grpc.testing.TestService/"] = mc2
	sc = grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc
	// Wait for the new service config to propagate.
	for {
		if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
			break
		}
	}
	// The following RPCs are expected to become fail-fast.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Unavailable {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.Unavailable)
	}
}

func (s) TestServiceConfigWaitForReadyTD(t *testing.T) {
	for _, e := range listTestEnv() {
		testServiceConfigWaitForReadyTD(t, e)
	}
}

func testServiceConfigWaitForReadyTD(t *testing.T, e env) {
	te, ch := testServiceConfigSetupTD(t, e)
	defer te.tearDown()

	// Case1: Client API set failfast to be false, and service config set wait_for_ready to be false, Client API should win, and the rpc will wait until deadline exceeds.
	mc := grpc.MethodConfig{
		WaitForReady: newBool(false),
		Timeout:      newDuration(time.Millisecond),
	}
	m := make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/EmptyCall"] = mc
	m["/grpc.testing.TestService/FullDuplexCall"] = mc
	sc := grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	if _, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}

	// Generate a service config update.
	// Case2: Client API does not set failfast, and service config set wait_for_ready to be true, and the rpc will wait until deadline exceeds.
	mc.WaitForReady = newBool(true)
	m = make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/EmptyCall"] = mc
	m["/grpc.testing.TestService/FullDuplexCall"] = mc
	sc = grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc

	// Wait for the new service config to take effect.
	mc = cc.GetMethodConfig("/grpc.testing.TestService/EmptyCall")
	for {
		if !*mc.WaitForReady {
			time.Sleep(100 * time.Millisecond)
			mc = cc.GetMethodConfig("/grpc.testing.TestService/EmptyCall")
			continue
		}
		break
	}
	// The following RPCs are expected to become non-fail-fast ones with 1ms deadline.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	if _, err := tc.FullDuplexCall(ctx); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
}

func (s) TestServiceConfigTimeoutTD(t *testing.T) {
	for _, e := range listTestEnv() {
		testServiceConfigTimeoutTD(t, e)
	}
}

func testServiceConfigTimeoutTD(t *testing.T, e env) {
	te, ch := testServiceConfigSetupTD(t, e)
	defer te.tearDown()

	// Case1: Client API sets timeout to be 1ns and ServiceConfig sets timeout to be 1hr. Timeout should be 1ns (min of 1ns and 1hr) and the rpc will wait until deadline exceeds.
	mc := grpc.MethodConfig{
		Timeout: newDuration(time.Hour),
	}
	m := make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/EmptyCall"] = mc
	m["/grpc.testing.TestService/FullDuplexCall"] = mc
	sc := grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	// The following RPCs are expected to become non-fail-fast ones with 1ns deadline.
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), time.Nanosecond)
	if _, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
	cancel()

	// Generate a service config update.
	// Case2: Client API sets timeout to be 1hr and ServiceConfig sets timeout to be 1ns. Timeout should be 1ns (min of 1ns and 1hr) and the rpc will wait until deadline exceeds.
	mc.Timeout = newDuration(time.Nanosecond)
	m = make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/EmptyCall"] = mc
	m["/grpc.testing.TestService/FullDuplexCall"] = mc
	sc = grpc.ServiceConfig{
		Methods: m,
	}
	ch <- sc

	// Wait for the new service config to take effect.
	mc = cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall")
	for {
		if *mc.Timeout != time.Nanosecond {
			time.Sleep(100 * time.Millisecond)
			mc = cc.GetMethodConfig("/grpc.testing.TestService/FullDuplexCall")
			continue
		}
		break
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}
	cancel()

	ctx, cancel = context.WithTimeout(context.Background(), time.Hour)
	if _, err := tc.FullDuplexCall(ctx, grpc.WaitForReady(true)); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/FullDuplexCall(_) = _, %v, want %s", err, codes.DeadlineExceeded)
	}
	cancel()
}

func (s) TestServiceConfigMaxMsgSizeTD(t *testing.T) {
	for _, e := range listTestEnv() {
		testServiceConfigMaxMsgSizeTD(t, e)
	}
}

func testServiceConfigMaxMsgSizeTD(t *testing.T, e env) {
	// Setting up values and objects shared across all test cases.
	const smallSize = 1
	const largeSize = 1024
	const extraLargeSize = 2048

	smallPayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, smallSize)
	if err != nil {
		t.Fatal(err)
	}
	largePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, largeSize)
	if err != nil {
		t.Fatal(err)
	}
	extraLargePayload, err := newPayload(testpb.PayloadType_COMPRESSABLE, extraLargeSize)
	if err != nil {
		t.Fatal(err)
	}

	mc := grpc.MethodConfig{
		MaxReqSize:  newInt(extraLargeSize),
		MaxRespSize: newInt(extraLargeSize),
	}

	m := make(map[string]grpc.MethodConfig)
	m["/grpc.testing.TestService/UnaryCall"] = mc
	m["/grpc.testing.TestService/FullDuplexCall"] = mc
	sc := grpc.ServiceConfig{
		Methods: m,
	}
	// Case1: sc set maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te1, ch1 := testServiceConfigSetupTD(t, e)
	te1.startServer(&testServer{security: e.security})
	defer te1.tearDown()

	ch1 <- sc
	tc := testpb.NewTestServiceClient(te1.clientConn())

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: int32(extraLargeSize),
		Payload:      smallPayload,
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	// Test for unary RPC recv.
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = extraLargePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	respParam := []*testpb.ResponseParameters{
		{
			Size: int32(extraLargeSize),
		},
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            smallPayload,
	}
	stream, err := tc.FullDuplexCall(te1.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = extraLargePayload
	stream, err = tc.FullDuplexCall(te1.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}

	// Case2: Client API set maxReqSize to 1024 (send), maxRespSize to 1024 (recv). Sc sets maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te2, ch2 := testServiceConfigSetupTD(t, e)
	te2.maxClientReceiveMsgSize = newInt(1024)
	te2.maxClientSendMsgSize = newInt(1024)
	te2.startServer(&testServer{security: e.security})
	defer te2.tearDown()
	ch2 <- sc
	tc = testpb.NewTestServiceClient(te2.clientConn())

	// Test for unary RPC recv.
	req.Payload = smallPayload
	req.ResponseSize = int32(largeSize)

	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	stream, err = tc.FullDuplexCall(te2.ctx)
	respParam[0].Size = int32(largeSize)
	sreq.Payload = smallPayload
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te2.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}

	// Case3: Client API set maxReqSize to 4096 (send), maxRespSize to 4096 (recv). Sc sets maxReqSize to 2048 (send), maxRespSize to 2048 (recv).
	te3, ch3 := testServiceConfigSetupTD(t, e)
	te3.maxClientReceiveMsgSize = newInt(4096)
	te3.maxClientSendMsgSize = newInt(4096)
	te3.startServer(&testServer{security: e.security})
	defer te3.tearDown()
	ch3 <- sc
	tc = testpb.NewTestServiceClient(te3.clientConn())

	// Test for unary RPC recv.
	req.Payload = smallPayload
	req.ResponseSize = int32(largeSize)

	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want <nil>", err)
	}

	req.ResponseSize = int32(extraLargeSize)
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for unary RPC send.
	req.Payload = largePayload
	req.ResponseSize = int32(smallSize)
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want <nil>", err)
	}

	req.Payload = extraLargePayload
	if _, err := tc.UnaryCall(ctx, req); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, error code: %s", err, codes.ResourceExhausted)
	}

	// Test for streaming RPC recv.
	stream, err = tc.FullDuplexCall(te3.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam[0].Size = int32(largeSize)
	sreq.Payload = smallPayload

	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = _, %v, want <nil>", stream, err)
	}

	respParam[0].Size = int32(extraLargeSize)

	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Recv() = _, %v, want _, error code: %s", stream, err, codes.ResourceExhausted)
	}

	// Test for streaming RPC send.
	respParam[0].Size = int32(smallSize)
	sreq.Payload = largePayload
	stream, err = tc.FullDuplexCall(te3.ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	sreq.Payload = extraLargePayload
	if err := stream.Send(sreq); err == nil || status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("%v.Send(%v) = %v, want _, error code: %s", stream, sreq, err, codes.ResourceExhausted)
	}
}

// TestMalformedStreamMethod starts a test server and sends an RPC with a
// malformed method name. The server should respond with an UNIMPLEMENTED status
// code in this case.
func (s) TestMalformedStreamMethod(t *testing.T) {
	const testMethod = "a-method-name-without-any-slashes"
	te := newTest(t, tcpClearRREnv)
	te.startServer(nil)
	defer te.tearDown()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	err := te.clientConn().Invoke(ctx, testMethod, nil, nil)
	if gotCode := status.Code(err); gotCode != codes.Unimplemented {
		t.Fatalf("Invoke with method %q, got code %s, want %s", testMethod, gotCode, codes.Unimplemented)
	}
}

func (s) TestMethodFromServerStream(t *testing.T) {
	const testMethod = "/package.service/method"
	e := tcpClearRREnv
	te := newTest(t, e)
	var method string
	var ok bool
	te.unknownHandler = func(srv interface{}, stream grpc.ServerStream) error {
		method, ok = grpc.MethodFromServerStream(stream)
		return nil
	}

	te.startServer(nil)
	defer te.tearDown()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	_ = te.clientConn().Invoke(ctx, testMethod, nil, nil)
	if !ok || method != testMethod {
		t.Fatalf("Invoke with method %q, got %q, %v, want %q, true", testMethod, method, ok, testMethod)
	}
}

func (s) TestInterceptorCanAccessCallOptions(t *testing.T) {
	e := tcpClearRREnv
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	type observedOptions struct {
		headers     []*metadata.MD
		trailers    []*metadata.MD
		peer        []*peer.Peer
		creds       []credentials.PerRPCCredentials
		failFast    []bool
		maxRecvSize []int
		maxSendSize []int
		compressor  []string
		subtype     []string
	}
	var observedOpts observedOptions
	populateOpts := func(opts []grpc.CallOption) {
		for _, o := range opts {
			switch o := o.(type) {
			case grpc.HeaderCallOption:
				observedOpts.headers = append(observedOpts.headers, o.HeaderAddr)
			case grpc.TrailerCallOption:
				observedOpts.trailers = append(observedOpts.trailers, o.TrailerAddr)
			case grpc.PeerCallOption:
				observedOpts.peer = append(observedOpts.peer, o.PeerAddr)
			case grpc.PerRPCCredsCallOption:
				observedOpts.creds = append(observedOpts.creds, o.Creds)
			case grpc.FailFastCallOption:
				observedOpts.failFast = append(observedOpts.failFast, o.FailFast)
			case grpc.MaxRecvMsgSizeCallOption:
				observedOpts.maxRecvSize = append(observedOpts.maxRecvSize, o.MaxRecvMsgSize)
			case grpc.MaxSendMsgSizeCallOption:
				observedOpts.maxSendSize = append(observedOpts.maxSendSize, o.MaxSendMsgSize)
			case grpc.CompressorCallOption:
				observedOpts.compressor = append(observedOpts.compressor, o.CompressorType)
			case grpc.ContentSubtypeCallOption:
				observedOpts.subtype = append(observedOpts.subtype, o.ContentSubtype)
			}
		}
	}

	te.unaryClientInt = func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		populateOpts(opts)
		return nil
	}
	te.streamClientInt = func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		populateOpts(opts)
		return nil, nil
	}

	defaults := []grpc.CallOption{
		grpc.WaitForReady(true),
		grpc.MaxCallRecvMsgSize(1010),
	}
	tc := testpb.NewTestServiceClient(te.clientConn(grpc.WithDefaultCallOptions(defaults...)))

	var headers metadata.MD
	var trailers metadata.MD
	var pr peer.Peer
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	tc.UnaryCall(ctx, &testpb.SimpleRequest{},
		grpc.MaxCallRecvMsgSize(100),
		grpc.MaxCallSendMsgSize(200),
		grpc.PerRPCCredentials(testPerRPCCredentials{}),
		grpc.Header(&headers),
		grpc.Trailer(&trailers),
		grpc.Peer(&pr))
	expected := observedOptions{
		failFast:    []bool{false},
		maxRecvSize: []int{1010, 100},
		maxSendSize: []int{200},
		creds:       []credentials.PerRPCCredentials{testPerRPCCredentials{}},
		headers:     []*metadata.MD{&headers},
		trailers:    []*metadata.MD{&trailers},
		peer:        []*peer.Peer{&pr},
	}

	if !reflect.DeepEqual(expected, observedOpts) {
		t.Errorf("unary call did not observe expected options: expected %#v, got %#v", expected, observedOpts)
	}

	observedOpts = observedOptions{} // reset

	tc.StreamingInputCall(ctx,
		grpc.WaitForReady(false),
		grpc.MaxCallSendMsgSize(2020),
		grpc.UseCompressor("comp-type"),
		grpc.CallContentSubtype("json"))
	expected = observedOptions{
		failFast:    []bool{false, true},
		maxRecvSize: []int{1010},
		maxSendSize: []int{2020},
		compressor:  []string{"comp-type"},
		subtype:     []string{"json"},
	}

	if !reflect.DeepEqual(expected, observedOpts) {
		t.Errorf("streaming call did not observe expected options: expected %#v, got %#v", expected, observedOpts)
	}
}

func (s) TestCompressorRegister(t *testing.T) {
	for _, e := range listTestEnv() {
		testCompressorRegister(t, e)
	}
}

func testCompressorRegister(t *testing.T, e env) {
	te := newTest(t, e)
	te.clientCompression = false
	te.serverCompression = false
	te.clientUseCompression = true

	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	// Unary call
	const argSize = 271828
	const respSize = 314159
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("something", "something"))
	if _, err := tc.UnaryCall(ctx, req); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, <nil>", err)
	}
	// Streaming RPC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want <nil>", tc, err)
	}
	respParam := []*testpb.ResponseParameters{
		{
			Size: 31415,
		},
	}
	payload, err = newPayload(testpb.PayloadType_COMPRESSABLE, int32(31415))
	if err != nil {
		t.Fatal(err)
	}
	sreq := &testpb.StreamingOutputCallRequest{
		ResponseType:       testpb.PayloadType_COMPRESSABLE,
		ResponseParameters: respParam,
		Payload:            payload,
	}
	if err := stream.Send(sreq); err != nil {
		t.Fatalf("%v.Send(%v) = %v, want <nil>", stream, sreq, err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("%v.Recv() = %v, want <nil>", stream, err)
	}
}

func (s) TestServeExitsWhenListenerClosed(t *testing.T) {
	ss := &stubserver.StubServer{
		EmptyCallF: func(context.Context, *testpb.Empty) (*testpb.Empty, error) {
			return &testpb.Empty{}, nil
		},
	}

	s := grpc.NewServer()
	defer s.Stop()
	testpb.RegisterTestServiceServer(s, ss)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	done := make(chan struct{})
	go func() {
		s.Serve(lis)
		close(done)
	}()

	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to dial server: %v", err)
	}
	defer cc.Close()
	c := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.EmptyCall(ctx, &testpb.Empty{}); err != nil {
		t.Fatalf("Failed to send test RPC to server: %v", err)
	}

	if err := lis.Close(); err != nil {
		t.Fatalf("Failed to close listener: %v", err)
	}
	const timeout = 5 * time.Second
	timer := time.NewTimer(timeout)
	select {
	case <-done:
		return
	case <-timer.C:
		t.Fatalf("Serve did not return after %v", timeout)
	}
}

// Service handler returns status with invalid utf8 message.
func (s) TestStatusInvalidUTF8Message(t *testing.T) {
	var (
		origMsg = string([]byte{0xff, 0xfe, 0xfd})
		wantMsg = "���"
	)

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			return nil, status.Errorf(codes.Internal, origMsg)
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Convert(err).Message() != wantMsg {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v (msg %q); want _, err with msg %q", err, status.Convert(err).Message(), wantMsg)
	}
}

// Service handler returns status with details and invalid utf8 message. Proto
// will fail to marshal the status because of the invalid utf8 message. Details
// will be dropped when sending.
func (s) TestStatusInvalidUTF8Details(t *testing.T) {
	grpctest.TLogger.ExpectError("transport: failed to marshal rpc status")

	var (
		origMsg = string([]byte{0xff, 0xfe, 0xfd})
		wantMsg = "���"
	)

	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, in *testpb.Empty) (*testpb.Empty, error) {
			st := status.New(codes.Internal, origMsg)
			st, err := st.WithDetails(&testpb.Empty{})
			if err != nil {
				return nil, err
			}
			return nil, st.Err()
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := ss.Client.EmptyCall(ctx, &testpb.Empty{})
	st := status.Convert(err)
	if st.Message() != wantMsg {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v (msg %q); want _, err with msg %q", err, st.Message(), wantMsg)
	}
	if len(st.Details()) != 0 {
		// Details should be dropped on the server side.
		t.Fatalf("RPC status contain details: %v, want no details", st.Details())
	}
}

func (s) TestClientDoesntDeadlockWhileWritingErrornousLargeMessages(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			continue
		}
		testClientDoesntDeadlockWhileWritingErrornousLargeMessages(t, e)
	}
}

func testClientDoesntDeadlockWhileWritingErrornousLargeMessages(t *testing.T, e env) {
	te := newTest(t, e)
	te.userAgent = testAppUA
	smallSize := 1024
	te.maxServerReceiveMsgSize = &smallSize
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, 1048576)
	if err != nil {
		t.Fatal(err)
	}
	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		Payload:      payload,
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second*10))
				defer cancel()
				if _, err := tc.UnaryCall(ctx, req); status.Code(err) != codes.ResourceExhausted {
					t.Errorf("TestService/UnaryCall(_,_) = _. %v, want code: %s", err, codes.ResourceExhausted)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func (s) TestRPCTimeout(t *testing.T) {
	for _, e := range listTestEnv() {
		testRPCTimeout(t, e)
	}
}

func testRPCTimeout(t *testing.T, e env) {
	te := newTest(t, e)
	te.startServer(&testServer{security: e.security, unaryCallSleepTime: 500 * time.Millisecond})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)

	const argSize = 2718
	const respSize = 314

	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, argSize)
	if err != nil {
		t.Fatal(err)
	}

	req := &testpb.SimpleRequest{
		ResponseType: testpb.PayloadType_COMPRESSABLE,
		ResponseSize: respSize,
		Payload:      payload,
	}
	for i := -1; i <= 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(i)*time.Millisecond)
		if _, err := tc.UnaryCall(ctx, req); status.Code(err) != codes.DeadlineExceeded {
			t.Fatalf("TestService/UnaryCallv(_, _) = _, %v; want <nil>, error code: %s", err, codes.DeadlineExceeded)
		}
		cancel()
	}
}

func (s) TestDisabledIOBuffers(t *testing.T) {
	payload, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(60000))
	if err != nil {
		t.Fatalf("Failed to create payload: %v", err)
	}
	req := &testpb.StreamingOutputCallRequest{
		Payload: payload,
	}
	resp := &testpb.StreamingOutputCallResponse{
		Payload: payload,
	}

	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			for {
				in, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					t.Errorf("stream.Recv() = _, %v, want _, <nil>", err)
					return err
				}
				if !reflect.DeepEqual(in.Payload.Body, payload.Body) {
					t.Errorf("Received message(len: %v) on server not what was expected(len: %v).", len(in.Payload.Body), len(payload.Body))
					return err
				}
				if err := stream.Send(resp); err != nil {
					t.Errorf("stream.Send(_)= %v, want <nil>", err)
					return err
				}

			}
		},
	}

	s := grpc.NewServer(grpc.WriteBufferSize(0), grpc.ReadBufferSize(0))
	testpb.RegisterTestServiceServer(s, ss)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go func() {
		s.Serve(lis)
	}()
	defer s.Stop()
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	cc, err := grpc.DialContext(dctx, lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithWriteBufferSize(0), grpc.WithReadBufferSize(0))
	if err != nil {
		t.Fatalf("Failed to dial server")
	}
	defer cc.Close()
	c := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.FullDuplexCall(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("Failed to send test RPC to server")
	}
	for i := 0; i < 10; i++ {
		if err := stream.Send(req); err != nil {
			t.Fatalf("stream.Send(_) = %v, want <nil>", err)
		}
		in, err := stream.Recv()
		if err != nil {
			t.Fatalf("stream.Recv() = _, %v, want _, <nil>", err)
		}
		if !reflect.DeepEqual(in.Payload.Body, payload.Body) {
			t.Fatalf("Received message(len: %v) on client not what was expected(len: %v).", len(in.Payload.Body), len(payload.Body))
		}
	}
	stream.CloseSend()
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("stream.Recv() = _, %v, want _, io.EOF", err)
	}
}

func (s) TestServerMaxHeaderListSizeClientUserViolation(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			continue
		}
		testServerMaxHeaderListSizeClientUserViolation(t, e)
	}
}

func testServerMaxHeaderListSizeClientUserViolation(t *testing.T, e env) {
	te := newTest(t, e)
	te.maxServerHeaderListSize = new(uint32)
	*te.maxServerHeaderListSize = 216
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	metadata.AppendToOutgoingContext(ctx, "oversize", string(make([]byte, 216)))
	var err error
	if err = verifyResultWithDelay(func() (bool, error) {
		if _, err = tc.EmptyCall(ctx, &testpb.Empty{}); err != nil && status.Code(err) == codes.Internal {
			return true, nil
		}
		return false, fmt.Errorf("tc.EmptyCall() = _, err: %v, want _, error code: %v", err, codes.Internal)
	}); err != nil {
		t.Fatal(err)
	}
}

func (s) TestClientMaxHeaderListSizeServerUserViolation(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler {
			continue
		}
		testClientMaxHeaderListSizeServerUserViolation(t, e)
	}
}

func testClientMaxHeaderListSizeServerUserViolation(t *testing.T, e env) {
	te := newTest(t, e)
	te.maxClientHeaderListSize = new(uint32)
	*te.maxClientHeaderListSize = 1 // any header server sends will violate
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc := te.clientConn()
	tc := testpb.NewTestServiceClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if err = verifyResultWithDelay(func() (bool, error) {
		if _, err = tc.EmptyCall(ctx, &testpb.Empty{}); err != nil && status.Code(err) == codes.Internal {
			return true, nil
		}
		return false, fmt.Errorf("tc.EmptyCall() = _, err: %v, want _, error code: %v", err, codes.Internal)
	}); err != nil {
		t.Fatal(err)
	}
}

func (s) TestServerMaxHeaderListSizeClientIntentionalViolation(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler || e.security == "tls" {
			continue
		}
		testServerMaxHeaderListSizeClientIntentionalViolation(t, e)
	}
}

func testServerMaxHeaderListSizeClientIntentionalViolation(t *testing.T, e env) {
	te := newTest(t, e)
	te.maxServerHeaderListSize = new(uint32)
	*te.maxServerHeaderListSize = 512
	te.startServer(&testServer{security: e.security})
	defer te.tearDown()

	cc, dw := te.clientConnWithConnControl()
	tc := &testServiceClientWrapper{TestServiceClient: testpb.NewTestServiceClient(cc)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want _, <nil>", tc, err)
	}
	rcw := dw.getRawConnWrapper()
	val := make([]string, 512)
	for i := range val {
		val[i] = "a"
	}
	// allow for client to send the initial header
	time.Sleep(100 * time.Millisecond)
	rcw.writeHeaders(http2.HeadersFrameParam{
		StreamID:      tc.getCurrentStreamID(),
		BlockFragment: rcw.encodeHeader("oversize", strings.Join(val, "")),
		EndStream:     false,
		EndHeaders:    true,
	})
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.Internal {
		t.Fatalf("stream.Recv() = _, %v, want _, error code: %v", err, codes.Internal)
	}
}

func (s) TestClientMaxHeaderListSizeServerIntentionalViolation(t *testing.T) {
	for _, e := range listTestEnv() {
		if e.httpHandler || e.security == "tls" {
			continue
		}
		testClientMaxHeaderListSizeServerIntentionalViolation(t, e)
	}
}

func testClientMaxHeaderListSizeServerIntentionalViolation(t *testing.T, e env) {
	te := newTest(t, e)
	te.maxClientHeaderListSize = new(uint32)
	*te.maxClientHeaderListSize = 200
	lw := te.startServerWithConnControl(&testServer{security: e.security, setHeaderOnly: true})
	defer te.tearDown()
	cc, _ := te.clientConnWithConnControl()
	tc := &testServiceClientWrapper{TestServiceClient: testpb.NewTestServiceClient(cc)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := tc.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("%v.FullDuplexCall(_) = _, %v, want _, <nil>", tc, err)
	}
	var i int
	var rcw *rawConnWrapper
	for i = 0; i < 100; i++ {
		rcw = lw.getLastConn()
		if rcw != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
		continue
	}
	if i == 100 {
		t.Fatalf("failed to create server transport after 1s")
	}

	val := make([]string, 200)
	for i := range val {
		val[i] = "a"
	}
	// allow for client to send the initial header.
	time.Sleep(100 * time.Millisecond)
	rcw.writeHeaders(http2.HeadersFrameParam{
		StreamID:      tc.getCurrentStreamID(),
		BlockFragment: rcw.encodeRawHeader("oversize", strings.Join(val, "")),
		EndStream:     false,
		EndHeaders:    true,
	})
	if _, err := stream.Recv(); err == nil || status.Code(err) != codes.Internal {
		t.Fatalf("stream.Recv() = _, %v, want _, error code: %v", err, codes.Internal)
	}
}

func (s) TestNetPipeConn(t *testing.T) {
	// This test will block indefinitely if grpc writes both client and server
	// prefaces without either reading from the Conn.
	pl := testutils.NewPipeListener()
	s := grpc.NewServer()
	defer s.Stop()
	ts := &funcServer{unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
		return &testpb.SimpleResponse{}, nil
	}}
	testpb.RegisterTestServiceServer(s, ts)
	go s.Serve(pl)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := grpc.DialContext(ctx, "", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDialer(pl.Dialer()))
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer cc.Close()
	client := testpb.NewTestServiceClient(cc)
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("UnaryCall(_) = _, %v; want _, nil", err)
	}
}

func (s) TestLargeTimeout(t *testing.T) {
	for _, e := range listTestEnv() {
		testLargeTimeout(t, e)
	}
}

func testLargeTimeout(t *testing.T, e env) {
	te := newTest(t, e)
	te.declareLogNoise("Server.processUnaryRPC failed to write status")

	ts := &funcServer{}
	te.startServer(ts)
	defer te.tearDown()
	tc := testpb.NewTestServiceClient(te.clientConn())

	timeouts := []time.Duration{
		time.Duration(math.MaxInt64), // will be (correctly) converted to
		// 2562048 hours, which overflows upon converting back to an int64
		2562047 * time.Hour, // the largest timeout that does not overflow
	}

	for i, maxTimeout := range timeouts {
		ts.unaryCall = func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			deadline, ok := ctx.Deadline()
			timeout := time.Until(deadline)
			minTimeout := maxTimeout - 5*time.Second
			if !ok || timeout < minTimeout || timeout > maxTimeout {
				t.Errorf("ctx.Deadline() = (now+%v), %v; want [%v, %v], true", timeout, ok, minTimeout, maxTimeout)
				return nil, status.Error(codes.OutOfRange, "deadline error")
			}
			return &testpb.SimpleResponse{}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), maxTimeout)
		defer cancel()

		if _, err := tc.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
			t.Errorf("case %v: UnaryCall(_) = _, %v; want _, nil", i, err)
		}
	}
}

// Proxies typically send GO_AWAY followed by connection closure a minute or so later. This
// test ensures that the connection is re-created after GO_AWAY and not affected by the
// subsequent (old) connection closure.
func (s) TestGoAwayThenClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	lis1, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	s1 := grpc.NewServer()
	defer s1.Stop()
	ts := &funcServer{
		unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
		fullDuplexCall: func(stream testpb.TestService_FullDuplexCallServer) error {
			if err := stream.Send(&testpb.StreamingOutputCallResponse{}); err != nil {
				t.Errorf("unexpected error from send: %v", err)
				return err
			}
			// Wait forever.
			_, err := stream.Recv()
			if err == nil {
				t.Error("expected to never receive any message")
			}
			return err
		},
	}
	testpb.RegisterTestServiceServer(s1, ts)
	go s1.Serve(lis1)

	conn2Established := grpcsync.NewEvent()
	lis2, err := listenWithNotifyingListener("tcp", "localhost:0", conn2Established)
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	s2 := grpc.NewServer()
	defer s2.Stop()
	testpb.RegisterTestServiceServer(s2, ts)

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{Addresses: []resolver.Address{
		{Addr: lis1.Addr().String()},
		{Addr: lis2.Addr().String()},
	}})
	cc, err := grpc.DialContext(ctx, r.Scheme()+":///", grpc.WithResolvers(r), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	defer cc.Close()

	client := testpb.NewTestServiceClient(cc)

	// We make a streaming RPC and do an one-message-round-trip to make sure
	// it's created on connection 1.
	//
	// We use a long-lived RPC because it will cause GracefulStop to send
	// GO_AWAY, but the connection doesn't get closed until the server stops and
	// the client receives the error.
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("FullDuplexCall(_) = _, %v; want _, nil", err)
	}
	if _, err = stream.Recv(); err != nil {
		t.Fatalf("unexpected error from first recv: %v", err)
	}

	go s2.Serve(lis2)

	// Send GO_AWAY to connection 1.
	go s1.GracefulStop()

	// Wait for the ClientConn to enter IDLE state.
	state := cc.GetState()
	for ; state != connectivity.Idle && cc.WaitForStateChange(ctx, state); state = cc.GetState() {
	}
	if state != connectivity.Idle {
		t.Fatalf("timed out waiting for IDLE channel state; last state = %v", state)
	}

	// Initiate another RPC to create another connection.
	if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("UnaryCall(_) = _, %v; want _, nil", err)
	}

	// Assert that connection 2 has been established.
	<-conn2Established.Done()

	// Close the listener for server2 to prevent it from allowing new connections.
	lis2.Close()

	// Close connection 1.
	s1.Stop()

	// Wait for client to close.
	if _, err = stream.Recv(); err == nil {
		t.Fatal("expected the stream to die, but got a successful Recv")
	}

	// Do a bunch of RPCs, make sure it stays stable. These should go to connection 2.
	for i := 0; i < 10; i++ {
		if _, err := client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
			t.Fatalf("UnaryCall(_) = _, %v; want _, nil", err)
		}
	}
}

func listenWithNotifyingListener(network, address string, event *grpcsync.Event) (net.Listener, error) {
	lis, err := net.Listen(network, address)
	if err != nil {
		return nil, err
	}
	return notifyingListener{connEstablished: event, Listener: lis}, nil
}

type notifyingListener struct {
	connEstablished *grpcsync.Event
	net.Listener
}

func (lis notifyingListener) Accept() (net.Conn, error) {
	defer lis.connEstablished.Fire()
	return lis.Listener.Accept()
}

func (s) TestRPCWaitsForResolver(t *testing.T) {
	te := testServiceConfigSetup(t, tcpClearRREnv)
	te.startServer(&testServer{security: tcpClearRREnv.security})
	defer te.tearDown()
	r := manual.NewBuilderWithScheme("whatever")

	te.resolverScheme = r.Scheme()
	cc := te.clientConn(grpc.WithResolvers(r))
	tc := testpb.NewTestServiceClient(cc)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// With no resolved addresses yet, this will timeout.
	if _, err := tc.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("TestService/EmptyCall(_, _) = _, %v, want _, %s", err, codes.DeadlineExceeded)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		time.Sleep(time.Second)
		r.UpdateState(resolver.State{
			Addresses: []resolver.Address{{Addr: te.srvAddr}},
			ServiceConfig: parseServiceConfig(t, r, `{
		    "methodConfig": [
		        {
		            "name": [
		                {
		                    "service": "grpc.testing.TestService",
		                    "method": "UnaryCall"
		                }
		            ],
                    "maxRequestMessageBytes": 0
		        }
		    ]
		}`)})
	}()
	// We wait a second before providing a service config and resolving
	// addresses.  So this will wait for that and then honor the
	// maxRequestMessageBytes it contains.
	if _, err := tc.UnaryCall(ctx, &testpb.SimpleRequest{ResponseType: testpb.PayloadType_UNCOMPRESSABLE}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, nil", err)
	}
	if got := ctx.Err(); got != nil {
		t.Fatalf("ctx.Err() = %v; want nil (deadline should be set short by service config)", got)
	}
	if _, err := tc.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("TestService/UnaryCall(_, _) = _, %v, want _, nil", err)
	}
}

func (s) TestHTTPHeaderFrameErrorHandlingHTTPMode(t *testing.T) {
	// Non-gRPC content-type fallback path.
	for httpCode := range transport.HTTPStatusConvTab {
		doHTTPHeaderTest(t, transport.HTTPStatusConvTab[int(httpCode)], []string{
			":status", fmt.Sprintf("%d", httpCode),
			"content-type", "text/html", // non-gRPC content type to switch to HTTP mode.
			"grpc-status", "1", // Make up a gRPC status error
			"grpc-status-details-bin", "???", // Make up a gRPC field parsing error
		})
	}

	// Missing content-type fallback path.
	for httpCode := range transport.HTTPStatusConvTab {
		doHTTPHeaderTest(t, transport.HTTPStatusConvTab[int(httpCode)], []string{
			":status", fmt.Sprintf("%d", httpCode),
			// Omitting content type to switch to HTTP mode.
			"grpc-status", "1", // Make up a gRPC status error
			"grpc-status-details-bin", "???", // Make up a gRPC field parsing error
		})
	}

	// Malformed HTTP status when fallback.
	doHTTPHeaderTest(t, codes.Internal, []string{
		":status", "abc",
		// Omitting content type to switch to HTTP mode.
		"grpc-status", "1", // Make up a gRPC status error
		"grpc-status-details-bin", "???", // Make up a gRPC field parsing error
	})
}

// Testing erroneous ResponseHeader or Trailers-only (delivered in the first HEADERS frame).
func (s) TestHTTPHeaderFrameErrorHandlingInitialHeader(t *testing.T) {
	for _, test := range []struct {
		header  []string
		errCode codes.Code
	}{
		{
			// missing gRPC status.
			header: []string{
				":status", "403",
				"content-type", "application/grpc",
			},
			errCode: codes.PermissionDenied,
		},
		{
			// malformed grpc-status.
			header: []string{
				":status", "502",
				"content-type", "application/grpc",
				"grpc-status", "abc",
			},
			errCode: codes.Internal,
		},
		{
			// Malformed grpc-tags-bin field.
			header: []string{
				":status", "502",
				"content-type", "application/grpc",
				"grpc-status", "0",
				"grpc-tags-bin", "???",
			},
			errCode: codes.Unavailable,
		},
		{
			// gRPC status error.
			header: []string{
				":status", "502",
				"content-type", "application/grpc",
				"grpc-status", "3",
			},
			errCode: codes.Unavailable,
		},
	} {
		doHTTPHeaderTest(t, test.errCode, test.header)
	}
}

// Testing non-Trailers-only Trailers (delivered in second HEADERS frame)
func (s) TestHTTPHeaderFrameErrorHandlingNormalTrailer(t *testing.T) {
	for _, test := range []struct {
		responseHeader []string
		trailer        []string
		errCode        codes.Code
	}{
		{
			responseHeader: []string{
				":status", "200",
				"content-type", "application/grpc",
			},
			trailer: []string{
				// trailer missing grpc-status
				":status", "502",
			},
			errCode: codes.Unavailable,
		},
		{
			responseHeader: []string{
				":status", "404",
				"content-type", "application/grpc",
			},
			trailer: []string{
				// malformed grpc-status-details-bin field
				"grpc-status", "0",
				"grpc-status-details-bin", "????",
			},
			errCode: codes.Unimplemented,
		},
		{
			responseHeader: []string{
				":status", "200",
				"content-type", "application/grpc",
			},
			trailer: []string{
				// malformed grpc-status-details-bin field
				"grpc-status", "0",
				"grpc-status-details-bin", "????",
			},
			errCode: codes.Internal,
		},
	} {
		doHTTPHeaderTest(t, test.errCode, test.responseHeader, test.trailer)
	}
}

func (s) TestHTTPHeaderFrameErrorHandlingMoreThanTwoHeaders(t *testing.T) {
	header := []string{
		":status", "200",
		"content-type", "application/grpc",
	}
	doHTTPHeaderTest(t, codes.Internal, header, header, header)
}

type httpServerResponse struct {
	headers  [][]string
	payload  []byte
	trailers [][]string
}

type httpServer struct {
	// If waitForEndStream is set, wait for the client to send a frame with end
	// stream in it before sending a response/refused stream.
	waitForEndStream bool
	refuseStream     func(uint32) bool
	responses        []httpServerResponse
}

func (s *httpServer) writeHeader(framer *http2.Framer, sid uint32, headerFields []string, endStream bool) error {
	if len(headerFields)%2 == 1 {
		panic("odd number of kv args")
	}

	var buf bytes.Buffer
	henc := hpack.NewEncoder(&buf)
	for len(headerFields) > 0 {
		k, v := headerFields[0], headerFields[1]
		headerFields = headerFields[2:]
		henc.WriteField(hpack.HeaderField{Name: k, Value: v})
	}

	return framer.WriteHeaders(http2.HeadersFrameParam{
		StreamID:      sid,
		BlockFragment: buf.Bytes(),
		EndStream:     endStream,
		EndHeaders:    true,
	})
}

func (s *httpServer) writePayload(framer *http2.Framer, sid uint32, payload []byte) error {
	return framer.WriteData(sid, false, payload)
}

func (s *httpServer) start(t *testing.T, lis net.Listener) {
	// Launch an HTTP server to send back header.
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Error accepting connection: %v", err)
			return
		}
		defer conn.Close()
		// Read preface sent by client.
		if _, err = io.ReadFull(conn, make([]byte, len(http2.ClientPreface))); err != nil {
			t.Errorf("Error at server-side while reading preface from client. Err: %v", err)
			return
		}
		reader := bufio.NewReader(conn)
		writer := bufio.NewWriter(conn)
		framer := http2.NewFramer(writer, reader)
		if err = framer.WriteSettingsAck(); err != nil {
			t.Errorf("Error at server-side while sending Settings ack. Err: %v", err)
			return
		}
		writer.Flush() // necessary since client is expecting preface before declaring connection fully setup.

		var sid uint32
		// Loop until conn is closed and framer returns io.EOF
		for requestNum := 0; ; requestNum = (requestNum + 1) % len(s.responses) {
			// Read frames until a header is received.
			for {
				frame, err := framer.ReadFrame()
				if err != nil {
					if err != io.EOF {
						t.Errorf("Error at server-side while reading frame. Err: %v", err)
					}
					return
				}
				sid = 0
				switch fr := frame.(type) {
				case *http2.HeadersFrame:
					// Respond after this if we are not waiting for an end
					// stream or if this frame ends it.
					if !s.waitForEndStream || fr.StreamEnded() {
						sid = fr.Header().StreamID
					}

				case *http2.DataFrame:
					// Respond after this if we were waiting for an end stream
					// and this frame ends it.  (If we were not waiting for an
					// end stream, this stream was already responded to when
					// the headers were received.)
					if s.waitForEndStream && fr.StreamEnded() {
						sid = fr.Header().StreamID
					}
				}
				if sid != 0 {
					if s.refuseStream == nil || !s.refuseStream(sid) {
						break
					}
					framer.WriteRSTStream(sid, http2.ErrCodeRefusedStream)
					writer.Flush()
				}
			}

			response := s.responses[requestNum]
			for _, header := range response.headers {
				if err = s.writeHeader(framer, sid, header, false); err != nil {
					t.Errorf("Error at server-side while writing headers. Err: %v", err)
					return
				}
				writer.Flush()
			}
			if response.payload != nil {
				if err = s.writePayload(framer, sid, response.payload); err != nil {
					t.Errorf("Error at server-side while writing payload. Err: %v", err)
					return
				}
				writer.Flush()
			}
			for i, trailer := range response.trailers {
				if err = s.writeHeader(framer, sid, trailer, i == len(response.trailers)-1); err != nil {
					t.Errorf("Error at server-side while writing trailers. Err: %v", err)
					return
				}
				writer.Flush()
			}
		}
	}()
}

func doHTTPHeaderTest(t *testing.T, errCode codes.Code, headerFields ...[]string) {
	t.Helper()
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen. Err: %v", err)
	}
	defer lis.Close()
	server := &httpServer{
		responses: []httpServerResponse{{trailers: headerFields}},
	}
	server.start(t, lis)
	cc, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to dial due to err: %v", err)
	}
	defer cc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := testpb.NewTestServiceClient(cc)
	stream, err := client.FullDuplexCall(ctx)
	if err != nil {
		t.Fatalf("error creating stream due to err: %v", err)
	}
	if _, err := stream.Recv(); err == nil || status.Code(err) != errCode {
		t.Fatalf("stream.Recv() = _, %v, want error code: %v", err, errCode)
	}
}

func (s) TestClientCancellationPropagatesUnary(t *testing.T) {
	wg := &sync.WaitGroup{}
	called, done := make(chan struct{}), make(chan struct{})
	ss := &stubserver.StubServer{
		EmptyCallF: func(ctx context.Context, _ *testpb.Empty) (*testpb.Empty, error) {
			close(called)
			<-ctx.Done()
			err := ctx.Err()
			if err != context.Canceled {
				t.Errorf("ctx.Err() = %v; want context.Canceled", err)
			}
			close(done)
			return nil, err
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	wg.Add(1)
	go func() {
		if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.Canceled {
			t.Errorf("ss.Client.EmptyCall() = _, %v; want _, Code()=codes.Canceled", err)
		}
		wg.Done()
	}()

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatalf("failed to perform EmptyCall after 10s")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("server failed to close done chan due to cancellation propagation")
	}
	wg.Wait()
}

type badGzipCompressor struct{}

func (badGzipCompressor) Do(w io.Writer, p []byte) error {
	buf := &bytes.Buffer{}
	gzw := gzip.NewWriter(buf)
	if _, err := gzw.Write(p); err != nil {
		return err
	}
	err := gzw.Close()
	bs := buf.Bytes()
	if len(bs) >= 6 {
		bs[len(bs)-6] ^= 1 // modify checksum at end by 1 byte
	}
	w.Write(bs)
	return err
}

func (badGzipCompressor) Type() string {
	return "gzip"
}

func (s) TestGzipBadChecksum(t *testing.T) {
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, _ *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
	}
	if err := ss.Start(nil, grpc.WithCompressor(badGzipCompressor{})); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := newPayload(testpb.PayloadType_COMPRESSABLE, int32(1024))
	if err != nil {
		t.Fatalf("Unexpected error from newPayload: %v", err)
	}
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{Payload: p}); err == nil ||
		status.Code(err) != codes.Internal ||
		!strings.Contains(status.Convert(err).Message(), gzip.ErrChecksum.Error()) {
		t.Errorf("ss.Client.UnaryCall(_) = _, %v\n\twant: _, status(codes.Internal, contains %q)", err, gzip.ErrChecksum)
	}
}

// When an RPC is canceled, it's possible that the last Recv() returns before
// all call options' after are executed.
func (s) TestCanceledRPCCallOptionRace(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			err := stream.Send(&testpb.StreamingOutputCallResponse{})
			if err != nil {
				return err
			}
			<-stream.Context().Done()
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	const count = 1000
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			var p peer.Peer
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			stream, err := ss.Client.FullDuplexCall(ctx, grpc.Peer(&p))
			if err != nil {
				t.Errorf("_.FullDuplexCall(_) = _, %v", err)
				return
			}
			if err := stream.Send(&testpb.StreamingOutputCallRequest{}); err != nil {
				t.Errorf("_ has error %v while sending", err)
				return
			}
			if _, err := stream.Recv(); err != nil {
				t.Errorf("%v.Recv() = %v", stream, err)
				return
			}
			cancel()
			if _, err := stream.Recv(); status.Code(err) != codes.Canceled {
				t.Errorf("%v compleled with error %v, want %s", stream, err, codes.Canceled)
				return
			}
			// If recv returns before call options are executed, peer.Addr is not set,
			// fail the test.
			if p.Addr == nil {
				t.Errorf("peer.Addr is nil, want non-nil")
				return
			}
		}()
	}
	wg.Wait()
}

func (s) TestClientSettingsFloodCloseConn(t *testing.T) {
	// Tests that the server properly closes its transport if the client floods
	// settings frames and then closes the connection.

	// Minimize buffer sizes to stimulate failure condition more quickly.
	s := grpc.NewServer(grpc.WriteBufferSize(20))
	l := bufconn.Listen(20)
	go s.Serve(l)

	// Dial our server and handshake.
	conn, err := l.Dial()
	if err != nil {
		t.Fatalf("Error dialing bufconn: %v", err)
	}

	n, err := conn.Write([]byte(http2.ClientPreface))
	if err != nil || n != len(http2.ClientPreface) {
		t.Fatalf("Error writing client preface: %v, %v", n, err)
	}

	fr := http2.NewFramer(conn, conn)
	f, err := fr.ReadFrame()
	if err != nil {
		t.Fatalf("Error reading initial settings frame: %v", err)
	}
	if _, ok := f.(*http2.SettingsFrame); ok {
		if err := fr.WriteSettingsAck(); err != nil {
			t.Fatalf("Error writing settings ack: %v", err)
		}
	} else {
		t.Fatalf("Error reading initial settings frame: type=%T", f)
	}

	// Confirm settings can be written, and that an ack is read.
	if err = fr.WriteSettings(); err != nil {
		t.Fatalf("Error writing settings frame: %v", err)
	}
	if f, err = fr.ReadFrame(); err != nil {
		t.Fatalf("Error reading frame: %v", err)
	}
	if sf, ok := f.(*http2.SettingsFrame); !ok || !sf.IsAck() {
		t.Fatalf("Unexpected frame: %v", f)
	}

	// Flood settings frames until a timeout occurs, indiciating the server has
	// stopped reading from the connection, then close the conn.
	for {
		conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		if err := fr.WriteSettings(); err != nil {
			if to, ok := err.(interface{ Timeout() bool }); !ok || !to.Timeout() {
				t.Fatalf("Received unexpected write error: %v", err)
			}
			break
		}
	}
	conn.Close()

	// If the server does not handle this situation correctly, it will never
	// close the transport.  This is because its loopyWriter.run() will have
	// exited, and thus not handle the goAway the draining process initiates.
	// Also, we would see a goroutine leak in this case, as the reader would be
	// blocked on the controlBuf's throttle() method indefinitely.

	timer := time.AfterFunc(5*time.Second, func() {
		t.Errorf("Timeout waiting for GracefulStop to return")
		s.Stop()
	})
	s.GracefulStop()
	timer.Stop()
}

// TestDeadlineSetOnConnectionOnClientCredentialHandshake tests that there is a deadline
// set on the net.Conn when a credential handshake happens in http2_client.
func (s) TestDeadlineSetOnConnectionOnClientCredentialHandshake(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	connCh := make(chan net.Conn, 1)
	go func() {
		defer close(connCh)
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Error accepting connection: %v", err)
			return
		}
		connCh <- conn
	}()
	defer func() {
		conn := <-connCh
		if conn != nil {
			conn.Close()
		}
	}()
	deadlineCh := testutils.NewChannel()
	cvd := &credentialsVerifyDeadline{
		deadlineCh: deadlineCh,
	}
	dOpt := grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		return &infoConn{Conn: conn}, nil
	})
	cc, err := grpc.Dial(lis.Addr().String(), dOpt, grpc.WithTransportCredentials(cvd))
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer cc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	deadline, err := deadlineCh.Receive(ctx)
	if err != nil {
		t.Fatalf("Error receiving from credsInvoked: %v", err)
	}
	// Default connection timeout is 20 seconds, so if the deadline exceeds now
	// + 18 seconds it should be valid.
	if !deadline.(time.Time).After(time.Now().Add(time.Second * 18)) {
		t.Fatalf("Connection did not have deadline set.")
	}
}

type infoConn struct {
	net.Conn
	deadline time.Time
}

func (c *infoConn) SetDeadline(t time.Time) error {
	c.deadline = t
	return c.Conn.SetDeadline(t)
}

type credentialsVerifyDeadline struct {
	deadlineCh *testutils.Channel
}

func (cvd *credentialsVerifyDeadline) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return rawConn, nil, nil
}

func (cvd *credentialsVerifyDeadline) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	cvd.deadlineCh.Send(rawConn.(*infoConn).deadline)
	return rawConn, nil, nil
}

func (cvd *credentialsVerifyDeadline) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{}
}
func (cvd *credentialsVerifyDeadline) Clone() credentials.TransportCredentials {
	return cvd
}
func (cvd *credentialsVerifyDeadline) OverrideServerName(s string) error {
	return nil
}

func unaryInterceptorVerifyConn(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	conn := transport.GetConnection(ctx)
	if conn == nil {
		return nil, status.Error(codes.NotFound, "connection was not in context")
	}
	return nil, status.Error(codes.OK, "")
}

// TestUnaryServerInterceptorGetsConnection tests whether the accepted conn on
// the server gets to any unary interceptors on the server side.
func (s) TestUnaryServerInterceptorGetsConnection(t *testing.T) {
	ss := &stubserver.StubServer{}
	if err := ss.Start([]grpc.ServerOption{grpc.UnaryInterceptor(unaryInterceptorVerifyConn)}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	if _, err := ss.Client.EmptyCall(ctx, &testpb.Empty{}); status.Code(err) != codes.OK {
		t.Fatalf("ss.Client.EmptyCall(_, _) = _, %v, want _, error code %s", err, codes.OK)
	}
}

func streamingInterceptorVerifyConn(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	conn := transport.GetConnection(ss.Context())
	if conn == nil {
		return status.Error(codes.NotFound, "connection was not in context")
	}
	return status.Error(codes.OK, "")
}

// TestStreamingServerInterceptorGetsConnection tests whether the accepted conn on
// the server gets to any streaming interceptors on the server side.
func (s) TestStreamingServerInterceptorGetsConnection(t *testing.T) {
	ss := &stubserver.StubServer{}
	if err := ss.Start([]grpc.ServerOption{grpc.StreamInterceptor(streamingInterceptorVerifyConn)}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	s, err := ss.Client.StreamingOutputCall(ctx, &testpb.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatalf("ss.Client.StreamingOutputCall(_) = _, %v, want _, <nil>", err)
	}
	if _, err := s.Recv(); err != io.EOF {
		t.Fatalf("ss.Client.StreamingInputCall(_) = _, %v, want _, %v", err, io.EOF)
	}
}

// unaryInterceptorVerifyAuthority verifies there is an unambiguous :authority
// once the request gets to an interceptor. An unambiguous :authority is defined
// as at most a single :authority header, and no host header according to A41.
func unaryInterceptorVerifyAuthority(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.NotFound, "metadata was not in context")
	}
	authority := md.Get(":authority")
	if len(authority) > 1 { // Should be an unambiguous authority by the time it gets to interceptor.
		return nil, status.Error(codes.NotFound, ":authority value had more than one value")
	}
	// Host header shouldn't be present by the time it gets to the interceptor
	// level (should either be renamed to :authority or explicitly deleted).
	host := md.Get("host")
	if len(host) != 0 {
		return nil, status.Error(codes.NotFound, "host header should not be present in metadata")
	}
	// Pass back the authority for verification on client - NotFound so
	// grpc-message will be available to read for verification.
	if len(authority) == 0 {
		// Represent no :authority header present with an empty string.
		return nil, status.Error(codes.NotFound, "")
	}
	return nil, status.Error(codes.NotFound, authority[0])
}

// TestAuthorityHeader tests that the eventual :authority that reaches the grpc
// layer is unambiguous due to logic added in A41.
func (s) TestAuthorityHeader(t *testing.T) {
	tests := []struct {
		name          string
		headers       []string
		wantAuthority string
	}{
		// "If :authority is missing, Host must be renamed to :authority." - A41
		{
			name: "Missing :authority",
			// Codepath triggered by incoming headers with no authority but with
			// a host.
			headers: []string{
				":method", "POST",
				":path", "/grpc.testing.TestService/UnaryCall",
				"content-type", "application/grpc",
				"te", "trailers",
				"host", "localhost",
			},
			wantAuthority: "localhost",
		},
		{
			name: "Missing :authority and host",
			// Codepath triggered by incoming headers with no :authority and no
			// host.
			headers: []string{
				":method", "POST",
				":path", "/grpc.testing.TestService/UnaryCall",
				"content-type", "application/grpc",
				"te", "trailers",
			},
			wantAuthority: "",
		},
		// "If :authority is present, Host must be discarded." - A41
		{
			name: ":authority and host present",
			// Codepath triggered by incoming headers with both an authority
			// header and a host header.
			headers: []string{
				":method", "POST",
				":path", "/grpc.testing.TestService/UnaryCall",
				":authority", "localhost",
				"content-type", "application/grpc",
				"host", "localhost2",
			},
			wantAuthority: "localhost",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			te := newTest(t, tcpClearRREnv)
			ts := &funcServer{unaryCall: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
				return &testpb.SimpleResponse{}, nil
			}}
			te.unaryServerInt = unaryInterceptorVerifyAuthority
			te.startServer(ts)
			defer te.tearDown()
			success := testutils.NewChannel()
			te.withServerTester(func(st *serverTester) {
				st.writeHeaders(http2.HeadersFrameParam{
					StreamID:      1,
					BlockFragment: st.encodeHeader(test.headers...),
					EndStream:     false,
					EndHeaders:    true,
				})
				st.writeData(1, true, []byte{0, 0, 0, 0, 0})

				for {
					frame := st.wantAnyFrame()
					f, ok := frame.(*http2.MetaHeadersFrame)
					if !ok {
						continue
					}
					for _, header := range f.Fields {
						if header.Name == "grpc-message" {
							success.Send(header.Value)
							return
						}
					}
				}
			})

			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			gotAuthority, err := success.Receive(ctx)
			if err != nil {
				t.Fatalf("Error receiving from channel: %v", err)
			}
			if gotAuthority != test.wantAuthority {
				t.Fatalf("gotAuthority: %v, wantAuthority %v", gotAuthority, test.wantAuthority)
			}
		})
	}
}

// wrapCloseListener tracks Accepts/Closes and maintains a counter of the
// number of open connections.
type wrapCloseListener struct {
	net.Listener
	connsOpen int32
}

// wrapCloseListener is returned by wrapCloseListener.Accept and decrements its
// connsOpen when Close is called.
type wrapCloseConn struct {
	net.Conn
	lis       *wrapCloseListener
	closeOnce sync.Once
}

func (w *wrapCloseListener) Accept() (net.Conn, error) {
	conn, err := w.Listener.Accept()
	if err != nil {
		return nil, err
	}
	atomic.AddInt32(&w.connsOpen, 1)
	return &wrapCloseConn{Conn: conn, lis: w}, nil
}

func (w *wrapCloseConn) Close() error {
	defer w.closeOnce.Do(func() { atomic.AddInt32(&w.lis.connsOpen, -1) })
	return w.Conn.Close()
}

// TestServerClosesConn ensures conn.Close is always closed even if the client
// doesn't complete the HTTP/2 handshake.
func (s) TestServerClosesConn(t *testing.T) {
	lis := bufconn.Listen(20)
	wrapLis := &wrapCloseListener{Listener: lis}

	s := grpc.NewServer()
	go s.Serve(wrapLis)
	defer s.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()

	for i := 0; i < 10; i++ {
		conn, err := lis.DialContext(ctx)
		if err != nil {
			t.Fatalf("Dial = _, %v; want _, nil", err)
		}
		conn.Close()
	}
	for ctx.Err() == nil {
		if atomic.LoadInt32(&wrapLis.connsOpen) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for conns to be closed by server; still open: %v", atomic.LoadInt32(&wrapLis.connsOpen))
}

// TestNilStatsHandler ensures we do not panic as a result of a nil stats
// handler.
func (s) TestNilStatsHandler(t *testing.T) {
	grpctest.TLogger.ExpectErrorN("ignoring nil parameter", 2)
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{grpc.StatsHandler(nil)}, grpc.WithStatsHandler(nil)); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{}); err != nil {
		t.Fatalf("Unexpected error from UnaryCall: %v", err)
	}
}

// TestUnexpectedEOF tests a scenario where a client invokes two unary RPC
// calls. The first call receives a payload which exceeds max grpc receive
// message length, and the second gets a large response. This second RPC should
// not fail with unexpected.EOF.
func (s) TestUnexpectedEOF(t *testing.T) {
	ss := &stubserver.StubServer{
		UnaryCallF: func(ctx context.Context, in *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
			return &testpb.SimpleResponse{
				Payload: &testpb.Payload{
					Body: bytes.Repeat([]byte("a"), int(in.ResponseSize)),
				},
			}, nil
		},
	}
	if err := ss.Start([]grpc.ServerOption{}); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	for i := 0; i < 10; i++ {
		// exceeds grpc.DefaultMaxRecvMessageSize, this should error with
		// RESOURCE_EXHAUSTED error.
		_, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{ResponseSize: 4194304})
		if code := status.Code(err); code != codes.ResourceExhausted {
			t.Fatalf("UnaryCall RPC returned error: %v, want status code %v", err, codes.ResourceExhausted)
		}
		// Larger response that doesn't exceed DefaultMaxRecvMessageSize, this
		// should work normally.
		if _, err := ss.Client.UnaryCall(ctx, &testpb.SimpleRequest{ResponseSize: 275075}); err != nil {
			t.Fatalf("UnaryCall RPC failed: %v", err)
		}
	}
}

// TestRecvWhileReturningStatus performs a Recv in a service handler while the
// handler returns its status.  A race condition could result in the server
// sending the first headers frame without the HTTP :status header.  This can
// happen when the failed Recv (due to the handler returning) and the handler's
// status both attempt to write the status, which would be the first headers
// frame sent, simultaneously.
func (s) TestRecvWhileReturningStatus(t *testing.T) {
	ss := &stubserver.StubServer{
		FullDuplexCallF: func(stream testpb.TestService_FullDuplexCallServer) error {
			// The client never sends, so this Recv blocks until the server
			// returns and causes stream operations to return errors.
			go stream.Recv()
			return nil
		},
	}
	if err := ss.Start(nil); err != nil {
		t.Fatalf("Error starting endpoint server: %v", err)
	}
	defer ss.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < 100; i++ {
		stream, err := ss.Client.FullDuplexCall(ctx)
		if err != nil {
			t.Fatalf("Error while creating stream: %v", err)
		}
		if _, err := stream.Recv(); err != io.EOF {
			t.Fatalf("stream.Recv() = %v, want io.EOF", err)
		}
	}
}
