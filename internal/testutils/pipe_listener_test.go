/*
 *
 * Copyright 2018 gRPC authors.
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

package testutils_test

import (
	"testing"
	"time"

	"google.golang.org/grpc/internal/testutils"
)

func TestPipeListener(t *testing.T) {
	pl := testutils.NewPipeListener()
	recvdBytes := make(chan []byte)
	want := "hello world"

	go func() {
		c, err := pl.Accept()
		if err != nil {
			t.Error(err)
		}

		read := make([]byte, len(want))
		_, err = c.Read(read)
		if err != nil {
			t.Error(err)
		}
		recvdBytes <- read
	}()

	dl := pl.Dialer()
	conn, err := dl("", time.Duration(0))
	if err != nil {
		t.Fatal(err)
	}

	_, err = conn.Write([]byte(want))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case gotBytes := <-recvdBytes:
		got := string(gotBytes)
		if got != want {
			t.Fatalf("expected to get %s, got %s", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to receive bytes")
	}
}

func TestAcceptUnblocksDial(t *testing.T) {
	pl := testutils.NewPipeListener()
	dialFinished := make(chan struct{})

	go func() {
		dl := pl.Dialer()
		if _, err := dl("", time.Duration(0)); err != nil {
			t.Error(err)
		}
		close(dialFinished)
	}()

	select {
	case <-dialFinished:
		t.Fatal("expected Dial to block until pl.Close or pl.Accept")
	default:
	}

	if _, err := pl.Accept(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-dialFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("expected Accept to unblock after pl.Accept was called")
	}
}

func TestCloseUnblocksDial(t *testing.T) {
	pl := testutils.NewPipeListener()
	acceptFinished := make(chan struct{})

	go func() {
		dl := pl.Dialer()
		if _, err := dl("", time.Duration(0)); err == nil {
			t.Error("expected to receive a 'conn closed' error from dial, got nil")
		}
		close(acceptFinished)
	}()

	select {
	case <-acceptFinished:
		t.Fatal("expected Accept to block until pl.Close or a dial occurred")
	default:
	}

	if err := pl.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-acceptFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("expected Accept to unblock after pl.Close was called")
	}
}

func TestDialUnblocksAccept(t *testing.T) {
	pl := testutils.NewPipeListener()
	acceptFinished := make(chan struct{})

	go func() {
		if _, err := pl.Accept(); err != nil {
			t.Error(err)
		}
		close(acceptFinished)
	}()

	select {
	case <-acceptFinished:
		t.Fatal("expected Accept to block until pl.Close or a dial occurred")
	default:
	}

	dl := pl.Dialer()
	if _, err := dl("", time.Duration(0)); err != nil {
		panic(err)
	}

	select {
	case <-acceptFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("expected Accept to unblock after Dial was called")
	}
}

func TestCloseUnblocksAccept(t *testing.T) {
	pl := testutils.NewPipeListener()
	acceptFinished := make(chan struct{})

	go func() {
		if _, err := pl.Accept(); err == nil {
			t.Error("expected to received a 'conn closed' error from dial, got nil")
		}
		close(acceptFinished)
	}()

	select {
	case <-acceptFinished:
		t.Fatal("expected Accept to block until pl.Close or a dial occurred")
	default:
	}

	if err := pl.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case <-acceptFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("expected Accept to unblock after pl.Close was called")
	}
}
