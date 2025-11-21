/*
 *
 * Copyright 2025 gRPC authors.
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

package grpc

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpcsync"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/transport"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/testdata"
)

func (s) TestDialWithTimeout(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis.Close()
	lisAddr := resolver.Address{Addr: lis.Addr().String()}
	lisDone := make(chan struct{})
	dialDone := make(chan struct{})
	// 1st listener accepts the connection and then does nothing
	go func() {
		defer close(lisDone)
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Error while accepting. Err: %v", err)
			return
		}
		framer := http2.NewFramer(conn, conn)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings. Err: %v", err)
			return
		}
		<-dialDone // Close conn only after dial returns.
	}()

	r := manual.NewBuilderWithScheme("whatever")
	r.InitialState(resolver.State{Addresses: []resolver.Address{lisAddr}})
	client, err := Dial(r.Scheme()+":///test.server", WithTransportCredentials(insecure.NewCredentials()), WithResolvers(r), WithTimeout(5*time.Second))
	close(dialDone)
	if err != nil {
		t.Fatalf("Dial failed. Err: %v", err)
	}
	defer client.Close()
	timeout := time.After(1 * time.Second)
	select {
	case <-timeout:
		t.Fatal("timed out waiting for server to finish")
	case <-lisDone:
	}
}

func (s) TestDialWaitsForServerSettings(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	defer lis.Close()
	done := make(chan struct{})
	sent := make(chan struct{})
	dialDone := make(chan struct{})
	go func() { // Launch the server.
		defer func() {
			close(done)
		}()
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("Error while accepting. Err: %v", err)
			return
		}
		defer conn.Close()
		// Sleep for a little bit to make sure that Dial on client
		// side blocks until settings are received.
		time.Sleep(100 * time.Millisecond)
		framer := http2.NewFramer(conn, conn)
		close(sent)
		if err := framer.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("Error while writing settings. Err: %v", err)
			return
		}
		<-dialDone // Close conn only after dial returns.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := DialContext(ctx, lis.Addr().String(), WithTransportCredentials(insecure.NewCredentials()), WithBlock())
	close(dialDone)
	if err != nil {
		t.Fatalf("Error while dialing. Err: %v", err)
	}
	defer client.Close()
	select {
	case <-sent:
	default:
		t.Fatalf("Dial returned before server settings were sent")
	}
	<-done
}

func (s) TestDialWaitsForServerSettingsAndFails(t *testing.T) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening. Err: %v", err)
	}
	done := make(chan struct{})
	numConns := 0
	go func() { // Launch the server.
		defer func() {
			close(done)
		}()
		for {
			conn, err := lis.Accept()
			if err != nil {
				break
			}
			numConns++
			defer conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	client, err := DialContext(ctx,
		lis.Addr().String(),
		WithTransportCredentials(insecure.NewCredentials()),
		WithReturnConnectionError(),
		WithConnectParams(ConnectParams{
			Backoff:           backoff.Config{},
			MinConnectTimeout: 250 * time.Millisecond,
		}))
	lis.Close()
	if err == nil {
		client.Close()
		t.Fatalf("Unexpected success (err=nil) while dialing")
	}
	expectedMsg := "server preface"
	if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) || !strings.Contains(err.Error(), expectedMsg) {
		t.Fatalf("DialContext(_) = %v; want a message that includes both %q and %q", err, context.DeadlineExceeded.Error(), expectedMsg)
	}
	<-done
	if numConns < 2 {
		t.Fatalf("dial attempts: %v; want > 1", numConns)
	}
}

func (s) TestWithTimeout(t *testing.T) {
	conn, err := Dial("passthrough:///Non-Existent.Server:80",
		WithTimeout(time.Millisecond),
		WithBlock(),
		WithTransportCredentials(insecure.NewCredentials()))
	if err == nil {
		conn.Close()
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("Dial(_, _) = %v, %v, want %v", conn, err, context.DeadlineExceeded)
	}
}

func (s) TestWithTransportCredentialsTLS(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	creds, err := credentials.NewClientTLSFromFile(testdata.Path("x509/server_ca_cert.pem"), "x.test.example.com")
	if err != nil {
		t.Fatalf("Failed to create credentials %v", err)
	}
	conn, err := DialContext(ctx, "passthrough:///Non-Existent.Server:80", WithTransportCredentials(creds), WithBlock())
	if err == nil {
		conn.Close()
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("Dial(_, _) = %v, %v, want %v", conn, err, context.DeadlineExceeded)
	}
}

func (s) TestDialContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DialContext(ctx, "Non-Existent.Server:80", WithBlock(), WithTransportCredentials(insecure.NewCredentials())); err != context.Canceled {
		t.Fatalf("DialContext(%v, _) = _, %v, want _, %v", ctx, err, context.Canceled)
	}
}

type failFastError struct{}

func (failFastError) Error() string   { return "failfast" }
func (failFastError) Temporary() bool { return false }

func (s) TestDialContextFailFast(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	failErr := failFastError{}
	dialer := func(string, time.Duration) (net.Conn, error) {
		return nil, failErr
	}

	_, err := DialContext(ctx, "Non-Existent.Server:80", WithBlock(), WithTransportCredentials(insecure.NewCredentials()), WithDialer(dialer), FailOnNonTempDialError(true))
	if terr, ok := err.(transport.ConnectionError); !ok || terr.Origin() != failErr {
		t.Fatalf("DialContext() = _, %v, want _, %v", err, failErr)
	}
}

func (s) TestClientUpdatesParamsAfterGoAway(t *testing.T) {
	grpctest.ExpectError("Client received GoAway with error code ENHANCE_YOUR_CALM and debug data equal to ASCII \"too_many_pings\"")

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Failed to listen. Err: %v", err)
	}
	defer lis.Close()
	connected := grpcsync.NewEvent()
	defer connected.Fire()
	go func() {
		conn, err := lis.Accept()
		if err != nil {
			t.Errorf("error accepting connection: %v", err)
			return
		}
		defer conn.Close()
		f := http2.NewFramer(conn, conn)
		// Start a goroutine to read from the conn to prevent the client from
		// blocking after it writes its preface.
		go func() {
			for {
				if _, err := f.ReadFrame(); err != nil {
					return
				}
			}
		}()
		if err := f.WriteSettings(http2.Setting{}); err != nil {
			t.Errorf("error writing settings: %v", err)
			return
		}
		<-connected.Done()
		if err := f.WriteGoAway(0, http2.ErrCodeEnhanceYourCalm, []byte("too_many_pings")); err != nil {
			t.Errorf("error writing GOAWAY: %v", err)
			return
		}
	}()
	addr := lis.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cc, err := DialContext(ctx, addr, WithBlock(), WithTransportCredentials(insecure.NewCredentials()), WithKeepaliveParams(keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             100 * time.Millisecond,
		PermitWithoutStream: true,
	}))
	if err != nil {
		t.Fatalf("DialContext(%s) failed: %v, want: nil", addr, err)
	}
	defer cc.Close()
	connected.Fire()
	for {
		time.Sleep(10 * time.Millisecond)
		cc.mu.RLock()
		v := cc.keepaliveParams.Time
		cc.mu.RUnlock()
		if v == 20*time.Second {
			// Success
			return
		}
		if ctx.Err() != nil {
			// Timeout
			t.Fatalf("cc.dopts.copts.Keepalive.Time = %v , want 20s", v)
		}
	}
}

// Test ensures that there is no panic if the attributes within
// resolver.State.Addresses contains a typed-nil value.
func (s) TestResolverAddressesWithTypedNilAttribute(t *testing.T) {
	r := manual.NewBuilderWithScheme(t.Name())
	resolver.Register(r)

	addrAttr := attributes.New("typed_nil", (*stringerVal)(nil))
	r.InitialState(resolver.State{Addresses: []resolver.Address{{Addr: "addr1", Attributes: addrAttr}}})

	cc, err := Dial(r.Scheme()+":///", WithTransportCredentials(insecure.NewCredentials()), WithResolvers(r))
	if err != nil {
		t.Fatalf("Unexpected error dialing: %v", err)
	}
	defer cc.Close()
}

type stringerVal struct{ s string }

func (s stringerVal) String() string { return s.s }

const errResolverBuilderScheme = "test-resolver-build-failure"

// errResolverBuilder is a resolver builder that returns an error from its Build
// method.
type errResolverBuilder struct {
	err error
}

func (b *errResolverBuilder) Build(resolver.Target, resolver.ClientConn, resolver.BuildOptions) (resolver.Resolver, error) {
	return nil, b.err
}

func (b *errResolverBuilder) Scheme() string {
	return errResolverBuilderScheme
}

// Tests that Dial returns an error if the resolver builder returns an error
// from its Build method.
func (s) TestDial_ResolverBuilder_Error(t *testing.T) {
	resolverErr := fmt.Errorf("resolver builder error")
	dopts := []DialOption{
		WithTransportCredentials(insecure.NewCredentials()),
		WithResolvers(&errResolverBuilder{err: resolverErr}),
	}
	_, err := Dial(errResolverBuilderScheme+":///test.server", dopts...)
	if err == nil {
		t.Fatalf("Dial() succeeded when it should have failed")
	}
	if !strings.Contains(err.Error(), resolverErr.Error()) {
		t.Fatalf("Dial() failed with error %v, want %v", err, resolverErr)
	}
}
