/*
 *
 * Copyright 2024 gRPC authors.
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
package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/resolver"
)

type clientPrefaceWrite struct {
	net.Conn
}

type clientPrefaceLength struct {
	net.Conn
}

type framerWriteSettings struct {
	net.Conn
}

type framerWriteWindowUpdate struct {
	net.Conn
}

func (hc *clientPrefaceWrite) Write(b []byte) (n int, err error) {
	return 0, errors.New("preface write error")
}

func (hc *clientPrefaceWrite) Close() error {
	return hc.Conn.Close()
}

func dialerClientPrefaceWrite(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &clientPrefaceWrite{Conn: conn}, nil
}

func (hc *clientPrefaceLength) Write(b []byte) (n int, err error) {

	incorrectPreface := "INCORRECT PREFACE\r\n\r\n"
	n, err = hc.Conn.Write([]byte(incorrectPreface))
	return n, err
}

func dialerClientPrefaceLength(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &clientPrefaceLength{Conn: conn}, nil
}

func (hc *framerWriteSettings) Write(b []byte) (n int, err error) {
	n, err = hc.Conn.Write(b)
	//compare framer value
	if n == 9 {
		return 0, errors.New("Framer write setting error")
	}
	return n, err
}

func dialerFramerWriteSettings(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &framerWriteSettings{Conn: conn}, nil
}

func (hc *framerWriteWindowUpdate) Write(b []byte) (n int, err error) {

	n, err = hc.Conn.Write(b)
	// compare for windowupdate value
	if n == 13 {
		return 0, errors.New("Framer write windowupdate error")
	}
	return n, err
}

func dialerFramerWriteWindowUpdate(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &framerWriteWindowUpdate{Conn: conn}, nil
}

func TestNewHTTP2ClientPrefaceFailure(t *testing.T) {

	// Create a server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TestNewHTTP2ClientPrefaceFailure panicked: %v", r)
		}
	}()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: dialerClientPrefaceWrite}, func(GoAwayReason) {})
	if err == nil {
		t.Error("Expected an error, but got nil")
	} else {
		if err.Error() != "connection error: desc = \"transport: failed to write client preface: preface write error\"" {
			t.Fatalf("Error while creating client transport: %v", err)
		}
	}
}

func TestNewHTTP2ClientPrefaceLengthFailure(t *testing.T) {
	// Create a server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TestNewHTTP2ClientPrefaceLengthFailure panicked: %v", r)
		}
	}()

	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: dialerClientPrefaceLength}, func(GoAwayReason) {})
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	} else {
		if err.Error() != "connection error: desc = \"transport: preface mismatch, wrote 21 bytes; want 24\"" {
			t.Fatalf("Error while creating client transport: %v", err)
		}
	}
}

func TestNewHTTP2ClientFramerWriteSettingsFailure(t *testing.T) {
	// Create a server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TestNewHTTP2ClientFramerWriteSettingsFailure panicked: %v", r)
		}
	}()

	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: dialerFramerWriteSettings}, func(GoAwayReason) {})
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	} else {
		if err.Error() != "connection error: desc = \"transport: failed to write initial settings frame: Framer write setting error\"" {
			t.Fatalf("Error while creating client transport: %v", err)
		}
	}
}

func TestNewHTTP2ClientFramerWriteWindowUpdateFailure(t *testing.T) {
	// Create a server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("TestNewHTTP2ClientFramerWriteWindowUpdateFailure panicked: %v", r)
		}
	}()

	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: dialerFramerWriteWindowUpdate, InitialConnWindowSize: 80000}, func(GoAwayReason) {})
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	} else {
		if err.Error() != "connection error: desc = \"transport: failed to write window update: Framer write windowupdate error\"" {
			t.Fatalf("Error while creating client transport: %v", err)
		}
	}
}
