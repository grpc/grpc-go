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
	"bytes"
	"context"
	"errors"
	"net"
	"testing"

	"golang.org/x/net/http2"
	"google.golang.org/grpc/resolver"
)

type Http2TestCustomError struct {
	Err string
}

func (hte Http2TestCustomError) Error() string {
	return hte.Err
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

func (cpl *clientPrefaceLengthConn) Write(b []byte) (n int, err error) {
	if bytes.Equal(b, []byte(http2.ClientPreface)) {
		incorrectPreface := "INCORRECT PREFACE\r\n\r\n"
		n, err = cpl.Conn.Write([]byte(incorrectPreface))
		return n, err
	}
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
	// compare b with serialized byte sequence of []http2.Setting{}
	if bytes.Equal(b, []byte{0, 0, 0, 4, 0, 0, 0, 0, 0}) {
		return 0, errors.New("force error for Framer write setting")
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
	// compare b with serialized byte sequence of http2.Setting{ID:  http2.SettingInitialWindowSize, Val: 14465}
	if bytes.Equal(b, []byte{0, 0, 4, 8, 0, 0, 0, 0, 0, 0, 0, 56, 129}) {
		return 0, errors.New("force error for windowupdate")
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
			name:     "client-preface-length",
			opts:     ConnectOptions{Dialer: dialerClientPrefaceLength},
			expected: "connection error: desc = \"transport: preface mismatch, wrote 21 bytes; want 24\"",
		},
		{
			name:     "framer-write-settings",
			opts:     ConnectOptions{Dialer: dialerFramerWriteSettings},
			expected: "connection error: desc = \"transport: failed to write initial settings frame: force error for Framer write setting\"",
		},
		{
			name:     "framer-write-windowUpdate",
			opts:     ConnectOptions{Dialer: dialerFramerWriteWindowUpdate, InitialConnWindowSize: 80000},
			expected: "connection error: desc = \"transport: failed to write window update: force error for windowupdate\"",
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
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, test.opts, func(GoAwayReason) {})
			if err == nil {
				t.Errorf("got nil, want an error")
			}
			expectedError := Http2TestCustomError{Err: test.expected}
			err = Http2TestCustomError{Err: err.Error()}
			if !errors.Is(err, expectedError) {
				t.Fatalf("TestNewHTTP2ClientTarget() = %s, want %s", err.Error(), test.expected)
			}
		})
	}
}
