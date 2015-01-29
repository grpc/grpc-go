/*
 *
 * Copyright 2014, Google Inc.
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

package grpc_test

import (
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/grpc-go/rpc"
	"github.com/google/grpc-go/rpc/codes"
	"github.com/google/grpc-go/rpc/credentials"
	"github.com/google/grpc-go/rpc/metadata"
	testpb "github.com/google/grpc-go/rpc/test/testdata"
	"golang.org/x/net/context"
)

var (
	testMetadata = metadata.MD{
		"key1": "value1",
		"key2": "value2",
	}
)

type mathServer struct {
}

func (s *mathServer) Div(ctx context.Context, in *testpb.DivArgs) (*testpb.DivReply, error) {
	md, ok := metadata.FromContext(ctx)
	if ok {
		if err := rpc.SendHeader(ctx, md); err != nil {
			log.Fatalf("rpc.SendHeader(%v, %v) = %v, want %v", ctx, md, err, nil)
		}
		rpc.SetTrailer(ctx, md)
	}
	n, d := in.GetDividend(), in.GetDivisor()
	if d == 0 {
		return nil, fmt.Errorf("math: divide by 0")
	}
	out := new(testpb.DivReply)
	out.Quotient = proto.Int64(n / d)
	out.Remainder = proto.Int64(n % d)
	// Simulate some service delay.
	time.Sleep(2 * time.Millisecond)
	return out, nil // no error
}

func (s *mathServer) DivMany(stream testpb.Math_DivManyServer) error {
	md, ok := metadata.FromContext(stream.Context())
	if ok {
		if err := stream.SendHeader(md); err != nil {
			log.Fatalf("%v.SendHeader(%v) = %v, want %v", stream, md, err, nil)
		}
		stream.SetTrailer(md)
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
		n, d := in.GetDividend(), in.GetDivisor()
		if d == 0 {
			return fmt.Errorf("math: divide by 0")
		}
		err = stream.Send(&testpb.DivReply{
			Quotient:  proto.Int64(n / d),
			Remainder: proto.Int64(n % d),
		})
		if err != nil {
			return err
		}
	}
}

func (s *mathServer) Fib(args *testpb.FibArgs, stream testpb.Math_FibServer) error {
	var (
		limit = args.GetLimit()
		count int64
		x, y  int64 = 0, 1
	)
	for count = 0; limit == 0 || count < limit; count++ {
		// Send the next number in the Fibonacci sequence.
		stream.Send(&testpb.Num{
			Num: proto.Int64(x),
		})
		x, y = y, x+y
	}
	return nil // The RPC library will call stream.CloseSend for us.
}

func (s *mathServer) Sum(stream testpb.Math_SumServer) error {
	var sum int64
	for {
		m, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&testpb.Num{Num: &sum})
		}
		if err != nil {
			return err
		}
		sum += m.GetNum()
	}
}

const tlsDir = "testdata/"

func setUp(useTLS bool, maxStream uint32) (s *rpc.Server, mc testpb.MathClient) {
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	_, port, err := net.SplitHostPort(lis.Addr().String())
	if err != nil {
		log.Fatalf("Failed to parse listener address: %v", err)
	}
	if useTLS {
		creds, err := credentials.NewServerTLSFromFile(tlsDir+"server1.pem", tlsDir+"server1.key")
		if err != nil {
			log.Fatalf("Failed to generate credentials %v", err)
		}
		s = rpc.NewServer(lis, rpc.MaxConcurrentStreams(maxStream), rpc.WithServerTLS(creds))
	} else {
		s = rpc.NewServer(lis, rpc.MaxConcurrentStreams(maxStream))
	}
	ms := &mathServer{}
	testpb.RegisterService(s, ms)
	go s.Run()
	addr := "localhost:" + port
	var conn *rpc.ClientConn
	if useTLS {
		creds, err := credentials.NewClientTLSFromFile(tlsDir+"ca.pem", "x.test.youtube.com")
		if err != nil {
			log.Fatalf("Failed to create credentials %v", err)
		}
		conn, err = rpc.Dial(addr, rpc.WithClientTLS(creds))
	} else {
		conn, err = rpc.Dial(addr)
	}
	if err != nil {
		log.Fatalf("Dial(%q) = %v", addr, err)
	}
	mc = testpb.NewMathClient(conn)
	return
}

func TestFailedRPC(t *testing.T) {
	s, mc := setUp(false, math.MaxUint32)
	defer s.Stop()
	args := &testpb.DivArgs{
		Dividend: proto.Int64(8),
		Divisor:  proto.Int64(0),
	}
	expectedErr := rpc.Errorf(codes.Unknown, "math: divide by 0")
	reply, rpcErr := mc.Div(context.Background(), args)
	if fmt.Sprint(rpcErr) != fmt.Sprint(expectedErr) {
		t.Fatalf(`mathClient.Div(_, _) = %v, %v; want <nil>, %v`, reply, rpcErr, expectedErr)
	}
}

func TestMetadataUnaryRPC(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	args := &testpb.DivArgs{
		Dividend: proto.Int64(8),
		Divisor:  proto.Int64(2),
	}
	ctx := metadata.NewContext(context.Background(), testMetadata)
	var header, trailer metadata.MD
	_, err := mc.Div(ctx, args, rpc.Header(&header), rpc.Trailer(&trailer))
	if err != nil {
		t.Fatalf("mathClient.Div(%v, _, _, _) = _, %v; want _, <nil>", ctx, err)
	}
	if !reflect.DeepEqual(testMetadata, header) {
		t.Fatalf("Received header metadata %v, want %v", header, testMetadata)
	}
	if !reflect.DeepEqual(testMetadata, trailer) {
		t.Fatalf("Received trailer metadata %v, want %v", trailer, testMetadata)
	}
}

func performOneRPC(t *testing.T, mc testpb.MathClient, wg *sync.WaitGroup) {
	args := &testpb.DivArgs{
		Dividend: proto.Int64(8),
		Divisor:  proto.Int64(3),
	}
	reply, err := mc.Div(context.Background(), args)
	want := &testpb.DivReply{
		Quotient:  proto.Int64(2),
		Remainder: proto.Int64(2),
	}
	if err != nil || !proto.Equal(reply, want) {
		t.Errorf(`mathClient.Div(_, _) = %v, %v; want %v, <nil>`, reply, err, want)
	}
	wg.Done()
}

// This test mimics a user who sends 1000 RPCs concurrently on a faulty transport.
// TODO(zhaoq): Refactor to make this clearer and add more cases to test racy
// and error-prone paths.
func TestRetry(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(1 * time.Second)
		// The server shuts down the network connection to make a
		// transport error which will be detected by the client side
		// code.
		s.CloseConns()
		wg.Done()
	}()
	// All these RPCs should succeed eventually.
	for i := 0; i < 1000; i++ {
		time.Sleep(2 * time.Millisecond)
		wg.Add(1)
		go performOneRPC(t, mc, &wg)
	}
	wg.Wait()
}

// TODO(zhaoq): Have a better test coverage of timeout and cancellation mechanism.
func TestTimeout(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	args := &testpb.DivArgs{
		Dividend: proto.Int64(8),
		Divisor:  proto.Int64(3),
	}
	// Performs 100 RPCs with various timeout values so that
	// the RPCs could timeout on different stages of their lifetime. This
	// is the best-effort to cover various cases when an rpc gets cancelled.
	for i := 1; i <= 100; i++ {
		ctx, _ := context.WithTimeout(context.Background(), time.Duration(i)*time.Microsecond)
		reply, err := mc.Div(ctx, args)
		if rpc.Code(err) != codes.DeadlineExceeded {
			t.Fatalf(`mathClient.Div(_, _) = %v, %v; want <nil>, error code: %d`, reply, err, codes.DeadlineExceeded)
		}
	}
}

func TestCancel(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	args := &testpb.DivArgs{
		Dividend: proto.Int64(8),
		Divisor:  proto.Int64(3),
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(1*time.Millisecond, cancel)
	reply, err := mc.Div(ctx, args)
	if rpc.Code(err) != codes.Canceled {
		t.Fatalf(`mathClient.Div(_, _) = %v, %v; want <nil>, error code: %d`, reply, err, codes.Canceled)
	}
}

// The following tests the gRPC streaming RPC implementations.
// TODO(zhaoq): Have better coverage on error cases.

func TestBidiStreaming(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	for _, test := range []struct {
		// input
		divs []string
		// output
		status error
	}{
		{[]string{"1/1", "3/2", "2/3", "1/2"}, io.EOF},
		{[]string{"2/5", "2/3", "3/0", "5/4"}, rpc.Errorf(codes.Unknown, "math: divide by 0")},
	} {
		stream, err := mc.DivMany(context.Background())
		if err != nil {
			t.Fatalf("failed to create stream %v", err)
		}
		// Start a goroutine to parse and send the args.
		go func() {
			for _, args := range parseArgs(test.divs) {
				if err := stream.Send(args); err != nil {
					t.Errorf("Send failed: ", err)
					return
				}
			}
			// Tell the server we're done sending args.
			stream.CloseSend()
		}()
		var rpcStatus error
		for {
			_, err := stream.Recv()
			if err != nil {
				rpcStatus = err
				break
			}
		}
		if rpcStatus != test.status {
			t.Fatalf(`mathClient.DivMany got %v ; want %v`, rpcStatus, test.status)
		}
	}
}

// parseArgs converts a list of "n/d" strings into DivArgs.
// parseArgs crashes the process on error.
func parseArgs(divs []string) (args []*testpb.DivArgs) {
	for _, div := range divs {
		parts := strings.Split(div, "/")
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		d, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		args = append(args, &testpb.DivArgs{
			Dividend: &n,
			Divisor:  &d,
		})
	}
	return
}

func TestMetadataStreamingRPC(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()
	ctx := metadata.NewContext(context.Background(), testMetadata)
	stream, err := mc.DivMany(ctx)
	if err != nil {
		t.Fatalf("Failed to create stream %v", err)
	}
	go func() {
		headerMD, err := stream.Header()
		if err != nil || !reflect.DeepEqual(testMetadata, headerMD) {
			t.Errorf("#1 %v.Header() = %v, %v, want %v, <nil>", stream, headerMD, err, testMetadata)
		}
		// test the cached value.
		headerMD, err = stream.Header()
		if err != nil || !reflect.DeepEqual(testMetadata, headerMD) {
			t.Errorf("#2 %v.Header() = %v, %v, want %v, <nil>", stream, headerMD, err, testMetadata)
		}
		for _, args := range parseArgs([]string{"1/1", "3/2", "2/3"}) {
			if err := stream.Send(args); err != nil {
				t.Errorf("%v.Send(_) failed: %v", stream, err)
				return
			}
		}
		// Tell the server we're done sending args.
		stream.CloseSend()
	}()
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}
	trailerMD := stream.Trailer()
	if !reflect.DeepEqual(testMetadata, trailerMD) {
		t.Fatalf("%v.Trailer() = %v, want %v", stream, trailerMD, testMetadata)
	}
}

func TestServerStreaming(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()

	args := &testpb.FibArgs{}
	// Requests the first 10 Fibonnaci numbers.
	args.Limit = proto.Int64(10)

	// Start the stream and send the args.
	stream, err := mc.Fib(context.Background(), args)
	if err != nil {
		t.Fatalf("failed to create stream %v", err)
	}
	var rpcStatus error
	for {
		_, err := stream.Recv()
		if err != nil {
			rpcStatus = err
			break
		}
	}
	if rpcStatus != io.EOF {
		t.Fatalf(`mathClient.Fib got %v ; want <EOF>`, rpcStatus)
	}
}

func TestClientStreaming(t *testing.T) {
	s, mc := setUp(true, math.MaxUint32)
	defer s.Stop()

	stream, err := mc.Sum(context.Background())
	if err != nil {
		t.Fatalf("failed to create stream: %v", err)
	}
	for _, n := range []int64{1, -2, 0, 7} {
		if err := stream.Send(&testpb.Num{Num: &n}); err != nil {
			t.Fatalf("failed to send requests %v", err)
		}
	}
	if _, err := stream.CloseAndRecv(); err != io.EOF {
		t.Fatalf("stream.CloseAndRecv() got %v; want <EOF>", err)
	}
}

func TestExceedMaxStreamsLimit(t *testing.T) {
	// Only allows 1 live stream per server transport.
	s, mc := setUp(true, 1)
	defer s.Stop()
	var err error
	for {
		time.Sleep(2 * time.Millisecond)
		_, err = mc.Sum(context.Background())
		// Loop until the settings of max concurrent streams is
		// received by the client.
		if err != nil {
			break
		}
	}
	if rpc.Code(err) != codes.Unavailable {
		t.Fatalf("got %v, want error code %d", err, codes.Unavailable)
	}
}
