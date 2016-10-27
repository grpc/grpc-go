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
	"testing"
	"net"
	"fmt"
	"golang.org/x/net/context"
	"crypto/tls"
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

func TestTLSClientHandshakeReturnsTLSInfo(t *testing.T) {
	localPort := ":5050"
	lis, err := net.Listen("tcp",localPort)
	if err != nil {
		t.Fatalf("Failed to start local server. Listener error: %v", err)
	}
	c := NewTLS(&tls.Config{InsecureSkipVerify: true, 
	         Certificates: []tls.Certificate{
		         tls.Certificate{},
	         },
		 CipherSuites: []uint16{
			 tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		 },
		 PreferServerCipherSuites: true,
		 MaxVersion: 0,
             })
	go func() {
		serverRawConn, _ := lis.Accept()
		serverConn := tls.Server(serverRawConn, c.(*tlsCreds).config)
		serverErr := serverConn.Handshake()
		if serverErr != nil {
			fmt.Println("error on server", serverErr)
		}
	}()
	defer lis.Close()
	conn, err := net.Dial("tcp", localPort)
	if err != nil {
		t.Fatalf("Failed to connect to local server. Error: %v", err)
	}
	fmt.Println("conn : ", conn)
	_, tlsInfo, err := c.ClientHandshake(context.Background(),localPort,conn)
	if err != nil {
		t.Fatalf("failed at client handshake. Error: %v", err)
	}
	if tlsInfo == nil {
		t.Fatalf("Failed to recieve auth info from client handshake.")
	}
}

