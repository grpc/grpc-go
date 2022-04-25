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

// Binary grpclb_fallback is an interop test client for grpclb fallback.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/grpclb"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/credentials/google"
	_ "google.golang.org/grpc/xds/googledirectpath"

	testgrpc "google.golang.org/grpc/interop/grpc_testing"
	testpb "google.golang.org/grpc/interop/grpc_testing"
)

var (
	customCredentialsType   = flag.String("custom_credentials_type", "", "Client creds to use")
	serverURI               = flag.String("server_uri", "dns:///staging-grpc-directpath-fallback-test.googleapis.com:443", "The server host name")
	induceFallbackCmd       = flag.String("induce_fallback_cmd", "", "Command to induce fallback e.g. by making certain addresses unroutable")
	fallbackDeadlineSeconds = flag.Int("fallback_deadline_seconds", 1, "How long to wait for fallback to happen after induce_fallback_cmd")
	testCase                = flag.String("test_case", "",
		`Configure different test cases. Valid options are:
        fallback_before_startup : LB/backend connections fail before RPC's have been made;
        fallback_after_startup : LB/backend connections fail after RPC's have been made;`)
	infoLog  = log.New(os.Stderr, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
	errorLog = log.New(os.Stderr, "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
)

func doRPCAndGetPath(client testgrpc.TestServiceClient, timeout time.Duration) testpb.GrpclbRouteType {
	infoLog.Printf("doRPCAndGetPath timeout:%v\n", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req := &testpb.SimpleRequest{
		FillGrpclbRouteType: true,
	}
	reply, err := client.UnaryCall(ctx, req)
	if err != nil {
		infoLog.Printf("doRPCAndGetPath error:%v\n", err)
		return testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_UNKNOWN
	}
	g := reply.GetGrpclbRouteType()
	infoLog.Printf("doRPCAndGetPath got grpclb route type: %v\n", g)
	if g != testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK && g != testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND {
		errorLog.Fatalf("Expected grpclb route type to be either backend or fallback; got: %d", g)
	}
	return g
}

func dialTCPUserTimeout(ctx context.Context, addr string) (net.Conn, error) {
	control := func(network, address string, c syscall.RawConn) error {
		var syscallErr error
		controlErr := c.Control(func(fd uintptr) {
			syscallErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, 20000)
		})
		if syscallErr != nil {
			errorLog.Fatalf("syscall error setting sockopt TCP_USER_TIMEOUT: %v", syscallErr)
		}
		if controlErr != nil {
			errorLog.Fatalf("control error setting sockopt TCP_USER_TIMEOUT: %v", syscallErr)
		}
		return nil
	}
	d := &net.Dialer{
		Control: control,
	}
	return d.DialContext(ctx, "tcp", addr)
}

func createTestConn() *grpc.ClientConn {
	opts := []grpc.DialOption{
		grpc.WithContextDialer(dialTCPUserTimeout),
	}
	switch *customCredentialsType {
	case "tls":
		creds := credentials.NewClientTLSFromCert(nil, "")
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case "alts":
		creds := alts.NewClientCreds(alts.DefaultClientOptions())
		opts = append(opts, grpc.WithTransportCredentials(creds))
	case "google_default_credentials":
		opts = append(opts, grpc.WithCredentialsBundle(google.NewDefaultCredentials()))
	case "compute_engine_channel_creds":
		opts = append(opts, grpc.WithCredentialsBundle(google.NewComputeEngineCredentials()))
	default:
		errorLog.Fatalf("Invalid --custom_credentials_type:%v", *customCredentialsType)
	}
	conn, err := grpc.Dial(*serverURI, opts...)
	if err != nil {
		errorLog.Fatalf("Fail to dial: %v", err)
	}
	return conn
}

func runCmd(command string) {
	infoLog.Printf("Running cmd:|%v|\n", command)
	if err := exec.Command("bash", "-c", command).Run(); err != nil {
		errorLog.Fatalf("error running cmd:|%v| : %v", command, err)
	}
}

func waitForFallbackAndDoRPCs(client testgrpc.TestServiceClient, fallbackDeadline time.Time) {
	fallbackRetryCount := 0
	fellBack := false
	for time.Now().Before(fallbackDeadline) {
		g := doRPCAndGetPath(client, 20*time.Second)
		if g == testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK {
			infoLog.Println("Made one successul RPC to a fallback. Now expect the same for the rest.")
			fellBack = true
			break
		} else if g == testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND {
			errorLog.Fatalf("Got RPC type backend. This suggests an error in test implementation")
		} else {
			infoLog.Println("Retryable RPC failure on iteration:", fallbackRetryCount)
		}
		fallbackRetryCount++
	}
	if !fellBack {
		infoLog.Fatalf("Didn't fall back before deadline: %v\n", fallbackDeadline)
	}
	for i := 0; i < 30; i++ {
		if g := doRPCAndGetPath(client, 20*time.Second); g != testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_FALLBACK {
			errorLog.Fatalf("Expected RPC to take grpclb route type FALLBACK. Got: %v", g)
		}
		time.Sleep(time.Second)
	}
}

func doFallbackBeforeStartup() {
	runCmd(*induceFallbackCmd)
	fallbackDeadline := time.Now().Add(time.Duration(*fallbackDeadlineSeconds) * time.Second)
	conn := createTestConn()
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)
	waitForFallbackAndDoRPCs(client, fallbackDeadline)
}

func doFallbackAfterStartup() {
	conn := createTestConn()
	defer conn.Close()
	client := testgrpc.NewTestServiceClient(conn)
	if g := doRPCAndGetPath(client, 20*time.Second); g != testpb.GrpclbRouteType_GRPCLB_ROUTE_TYPE_BACKEND {
		errorLog.Fatalf("Expected RPC to take grpclb route type BACKEND. Got: %v", g)
	}
	runCmd(*induceFallbackCmd)
	fallbackDeadline := time.Now().Add(time.Duration(*fallbackDeadlineSeconds) * time.Second)
	waitForFallbackAndDoRPCs(client, fallbackDeadline)
}

func main() {
	flag.Parse()
	if len(*induceFallbackCmd) == 0 {
		errorLog.Fatalf("--induce_fallback_cmd unset")
	}
	switch *testCase {
	case "fallback_before_startup":
		doFallbackBeforeStartup()
		log.Printf("FallbackBeforeStartup done!\n")
	case "fallback_after_startup":
		doFallbackAfterStartup()
		log.Printf("FallbackAfterStartup done!\n")
	default:
		errorLog.Fatalf("Unsupported test case: %v", *testCase)
	}
}
