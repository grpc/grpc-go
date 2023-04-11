/*
 *
 * Copyright 2023 gRPC authors.
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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

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
	tc := testgrpc.NewTestServiceClient(cc)
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
	tc := testgrpc.NewTestServiceClient(cc)
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
	tc := testgrpc.NewTestServiceClient(cc)
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
	tc := testgrpc.NewTestServiceClient(te1.clientConn())

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
	tc = testgrpc.NewTestServiceClient(te2.clientConn())

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
	tc = testgrpc.NewTestServiceClient(te3.clientConn())

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
