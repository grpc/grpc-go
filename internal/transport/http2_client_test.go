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

	"golang.org/x/net/http2"
	"google.golang.org/grpc/resolver"
)

// ClientPreface is the HTTP/2 client preface string.
var ClientPreface = []byte(http2.ClientPreface)

type clientPrefaceConn struct {
	net.Conn
}

type clientPrefaceLengthConn struct {
	net.Conn
}

type framerWriteSettingsConn struct {
	net.Conn
}

type framerWindowUpdateConn struct {
	net.Conn
}

func (cp *clientPrefaceConn) Write(b []byte) (n int, err error) {
	if string(b) == string(ClientPreface) {
		return 0, errors.New("preface write error")
	}

	// Normally write the bytes if they don't match the ClientPreface
	return cp.Conn.Write(b)
}

func (cp *clientPrefaceConn) Close() error {
	return cp.Conn.Close()
}

func dialerClientPrefaceWrite(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &clientPrefaceConn{Conn: conn}, nil
}

func (cpl *clientPrefaceLengthConn) Write(b []byte) (n int, err error) {
	if string(b) == string(ClientPreface) {
		incorrectPreface := "INCORRECT PREFACE\r\n\r\n"
		n, err = cpl.Conn.Write([]byte(incorrectPreface))
		return n, err
	}
	// Normally write the bytes if they don't match the ClientPreface
	return cpl.Conn.Write(b)

}

func dialerClientPrefaceLength(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &clientPrefaceLengthConn{Conn: conn}, nil
}

func (fws *framerWriteSettingsConn) Write(b []byte) (n int, err error) {
	if string(b) != string(ClientPreface) {
		framerValue := 9
		n, err = fws.Conn.Write(b)
		// Compare the number of bytes written with the framer value
		if n == framerValue {
			return 0, errors.New("Framer write setting error")
		}

		return n, err
	}
	return fws.Conn.Write(b)
}

func dialerFramerWriteSettings(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &framerWriteSettingsConn{Conn: conn}, nil
}

func (fwu *framerWindowUpdateConn) Write(b []byte) (n int, err error) {
	if string(b) != string(ClientPreface) {
		// Simulate a WINDOW_UPDATE frame's value (window size in bytes)
		windowUpdateValue := 13
		n, err = fwu.Conn.Write(b)

		// Compare the number of bytes written with the WINDOW_UPDATE value
		if n == windowUpdateValue {
			return 0, errors.New("Framer write windowupdate error")
		}
		return n, err
	}
	return fwu.Conn.Write(b)

}

func dialerFramerWriteWindowUpdate(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &framerWindowUpdateConn{Conn: conn}, nil
}

func (s) TestNewHTTP2ClientTarget(t *testing.T) {
	tests := []struct {
		name     string
		opts     ConnectOptions
		expected string
	}{
		{
			name:     "client-preface-write",
			opts:     ConnectOptions{Dialer: dialerClientPrefaceWrite},
			expected: "connection error: desc = \"transport: failed to write client preface: preface write error\"",
		},
		{
			name:     "client-preface-length",
			opts:     ConnectOptions{Dialer: dialerClientPrefaceLength},
			expected: "connection error: desc = \"transport: preface mismatch, wrote 21 bytes; want 24\"",
		},
		{
			name:     "framer-write-settings",
			opts:     ConnectOptions{Dialer: dialerFramerWriteSettings},
			expected: "connection error: desc = \"transport: failed to write initial settings frame: Framer write setting error\"",
		},
		{
			name:     "framer-write-windowUpdate",
			opts:     ConnectOptions{Dialer: dialerFramerWriteWindowUpdate, InitialConnWindowSize: 80000},
			expected: "connection error: desc = \"transport: failed to write window update: Framer write windowupdate error\"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			// Create a server.
			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("Listen() = _, %v, want _, <nil>", err)
			}
			defer lis.Close()
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
			defer cancel()

			_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, test.opts, func(GoAwayReason) {})
			if err == nil {
				t.Errorf("Expected an error, but got nil")
			} else {
				if err.Error() != test.expected {
					t.Fatalf("TestNewHTTP2ClientTarget() = %s, want %s", err.Error(), test.expected)
				}
			}
		})
	}
}