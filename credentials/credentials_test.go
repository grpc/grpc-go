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

package credentials

import (
	"crypto/tls"
	"net"
	"testing"

	"golang.org/x/net/context"
)

func TestTLSOverrideServerName(t *testing.T) {
	expectedServerName := "server.name"
	c := NewTLS(nil)
	c.OverrideServerName(expectedServerName)
	if c.Info().ServerName != expectedServerName {
		t.Fatalf("c.Info().ServerName = %v, want %v", c.Info().ServerName, expectedServerName)
	}
}

func TestTLSClone(t *testing.T) {
	expectedServerName := "server.name"
	c := NewTLS(nil)
	c.OverrideServerName(expectedServerName)
	cc := c.Clone()
	if cc.Info().ServerName != expectedServerName {
		t.Fatalf("cc.Info().ServerName = %v, want %v", cc.Info().ServerName, expectedServerName)
	}
	cc.OverrideServerName("")
	if c.Info().ServerName != expectedServerName {
		t.Fatalf("Change in clone should not affect the original, c.Info().ServerName = %v, want %v", c.Info().ServerName, expectedServerName)
	}

}

const tlsDir = "../test/testdata/"

type serverHandshake func(net.Conn, *tls.ConnectionState) error

func TestClientHandshakeReturnsAuthInfo(t *testing.T) {
	var serverConnState tls.ConnectionState
	errChan := make(chan error, 1)
	lisAddr, err := launchServer(t, &serverConnState, tlsServerHandshake, errChan)
	if err != nil {
		return
	}
	clientConnState, err := clientHandle(t, gRPCClientHandshake, lisAddr)
	if err != nil {
		return
	}
	// wait until server has populated the serverAuthInfo struct or failed.
	if err = <-errChan; err != nil {
		return
	}
	if !isEqualState(clientConnState, serverConnState) {
		t.Fatalf("c.ClientHandshake(_, %v, _) = %v, want %v.", lisAddr, clientConnState, serverConnState)
	}
}

func TestServerHandshakeReturnsAuthInfo(t *testing.T) {
	var serverConnState tls.ConnectionState
	errChan := make(chan error, 1)
	lisAddr, err := launchServer(t, &serverConnState, gRPCServerHandshake, errChan)
	if err != nil {
		return
	}
	clientConnState, err := clientHandle(t, tlsClientHandshake, lisAddr)
	if err != nil {
		return
	}
	// wait until server has populated the serverAuthInfo struct or failed.
	if err = <-errChan; err != nil {
		return
	}
	if !isEqualState(clientConnState, serverConnState) {
		t.Fatalf("ServerHandshake(_) = %v, want %v.", serverConnState, clientConnState)
	}
}

func TestServerAndClientHandshake(t *testing.T) {
	var serverConnState tls.ConnectionState
	errChan := make(chan error, 1)
	lisAddr, err := launchServer(t, &serverConnState, gRPCServerHandshake, errChan)
	if err != nil {
		return
	}
	clientConnState, err := clientHandle(t, gRPCClientHandshake, lisAddr)
	if err != nil {
		return
	}
	// wait until server has populated the serverAuthInfo struct or failed.
	if err = <-errChan; err != nil {
		return
	}
	if !isEqualState(clientConnState, serverConnState) {
		t.Fatalf("Connection states returened by server: %v and client: %v aren't same", serverConnState, clientConnState)
	}
}

func isEqualState(state1, state2 tls.ConnectionState) bool {
	if state1.Version == state2.Version &&
		state1.HandshakeComplete == state2.HandshakeComplete &&
		state1.CipherSuite == state2.CipherSuite &&
		state1.NegotiatedProtocol == state2.NegotiatedProtocol {
		return true
	}
	return false
}

func launchServer(t *testing.T, serverConnState *tls.ConnectionState, hs serverHandshake, errChan chan error) (string, error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Errorf("Failed to listen: %v", err)
		return "", err
	}
	go serverHandle(t, hs, serverConnState, errChan, lis)
	return lis.Addr().String(), nil
}

// Is run in a seperate go routine.
func serverHandle(t *testing.T, hs func(net.Conn, *tls.ConnectionState) error, serverConnState *tls.ConnectionState, errChan chan error, lis net.Listener) {
	defer lis.Close()
	var err error
	defer func() {
		errChan <- err
	}()
	serverRawConn, err := lis.Accept()
	if err != nil {
		t.Errorf("Server failed to accept connection: %v", err)
		return
	}
	err = hs(serverRawConn, serverConnState)
	if err != nil {
		t.Errorf("Error at server-side while handshake. Error: %v", err)
		return
	}
}

func clientHandle(t *testing.T, hs func(net.Conn, string) (tls.ConnectionState, error), lisAddr string) (tls.ConnectionState, error) {
	conn, err := net.Dial("tcp", lisAddr)
	if err != nil {
		t.Errorf("Client failed to connect to %s. Error: %v", lisAddr, err)
		return tls.ConnectionState{}, err
	}
	defer conn.Close()
	clientConnState, err := hs(conn, lisAddr)
	if err != nil {
		t.Errorf("Error on client while handshake. Error: %v", err)
	}
	return clientConnState, err
}

// Server handshake implementation using gRPC.
func gRPCServerHandshake(conn net.Conn, serverConnState *tls.ConnectionState) error {
	serverTLS, err := NewServerTLSFromFile(tlsDir+"server1.pem", tlsDir+"server1.key")
	if err != nil {
		return err
	}
	_, serverAuthInfo, err := serverTLS.ServerHandshake(conn)
	if err != nil {
		return err
	}
	*serverConnState = serverAuthInfo.(TLSInfo).State
	return nil
}

// Client handshake implementation using gRPC.
func gRPCClientHandshake(conn net.Conn, lisAddr string) (tls.ConnectionState, error) {
	clientTLS := NewTLS(&tls.Config{InsecureSkipVerify: true})
	_, authInfo, err := clientTLS.ClientHandshake(context.Background(), lisAddr, conn)
	if err != nil {
		return tls.ConnectionState{}, err
	}
	return authInfo.(TLSInfo).State, nil
}

// Server handshake implementation using tls.
func tlsServerHandshake(conn net.Conn, serverConnState *tls.ConnectionState) error {
	cert, err := tls.LoadX509KeyPair(tlsDir+"server1.pem", tlsDir+"server1.key")
	if err != nil {
		return err
	}
	serverTLSConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	serverConn := tls.Server(conn, serverTLSConfig)
	err = serverConn.Handshake()
	if err != nil {
		return err
	}
	*serverConnState = serverConn.ConnectionState()
	return nil
}

// Client handskae implementation using tls.
func tlsClientHandshake(conn net.Conn, _ string) (tls.ConnectionState, error) {
	clientTLSConfig := &tls.Config{InsecureSkipVerify: true}
	clientConn := tls.Client(conn, clientTLSConfig)
	err := clientConn.Handshake()
	if err != nil {
		return tls.ConnectionState{}, err
	}
	return clientConn.ConnectionState(), nil
}
