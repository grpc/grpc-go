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

package conn

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	core "google.golang.org/grpc/credentials/alts/internal"
	"google.golang.org/grpc/internal/grpctest"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

const (
	rekeyRecordProtocol = "ALTSRP_GCM_AES128_REKEY"
)

var (
	recordProtocols = []string{rekeyRecordProtocol}
	altsRecordFuncs = map[string]ALTSRecordFunc{
		// ALTS handshaker protocols.
		rekeyRecordProtocol: func(s core.Side, keyData []byte) (ALTSRecordCrypto, error) {
			return NewAES128GCM(s, keyData)
		},
	}
)

func init() {
	for protocol, f := range altsRecordFuncs {
		if err := RegisterProtocol(protocol, f); err != nil {
			panic(err)
		}
	}
}

// testConn mimics a net.Conn to the peer.
type testConn struct {
	net.Conn
	in  *bytes.Buffer
	out *bytes.Buffer
}

func (c *testConn) Read(b []byte) (n int, err error) {
	return c.in.Read(b)
}

func (c *testConn) Write(b []byte) (n int, err error) {
	return c.out.Write(b)
}

func (c *testConn) Close() error {
	return nil
}

func newTestALTSRecordConn(in, out *bytes.Buffer, side core.Side, rp string, protected []byte) *conn {
	key := []byte{
		// 16 arbitrary bytes.
		0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xd2, 0x4c, 0xce, 0x4f, 0x49}
	tc := testConn{
		in:  in,
		out: out,
	}
	c, err := NewConn(&tc, side, rp, key, protected)
	if err != nil {
		panic(fmt.Sprintf("Unexpected error creating test ALTS record connection: %v", err))
	}
	return c.(*conn)
}

func newConnPair(rp string, clientProtected []byte, serverProtected []byte) (client, server *conn) {
	clientBuf := new(bytes.Buffer)
	serverBuf := new(bytes.Buffer)
	clientConn := newTestALTSRecordConn(clientBuf, serverBuf, core.ClientSide, rp, clientProtected)
	serverConn := newTestALTSRecordConn(serverBuf, clientBuf, core.ServerSide, rp, serverProtected)
	return clientConn, serverConn
}

// newTCPConnPair returns a pair of conns backed by TCP over loopback.
func newTCPConnPair(rp string, clientProtected []byte, serverProtected []byte) (*conn, *conn, error) {
	const address = "localhost:50935"

	// Start the server.
	serverChan := make(chan net.Conn)
	listenChan := make(chan struct{})
	go func() {
		listener, err := net.Listen("tcp4", address)
		if err != nil {
			panic(fmt.Sprintf("failed to listen: %v", err))
		}
		defer listener.Close()
		listenChan <- struct{}{}
		conn, err := listener.Accept()
		if err != nil {
			panic(fmt.Sprintf("failed to aceept: %v", err))
		}
		serverChan <- conn
	}()

	// Ensure the server is listening before trying to connect.
	<-listenChan
	clientTCP, err := net.DialTimeout("tcp4", address, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to Dial: %w", err)
	}

	// Get the server-side connection returned by Accept().
	var serverTCP net.Conn
	select {
	case serverTCP = <-serverChan:
	case <-time.After(5 * time.Second):
		return nil, nil, fmt.Errorf("timed out waiting for server conn")
	}

	// Make the connection behave a little bit like a real one by imposing
	// an MTU.
	clientTCP = &mtuConn{clientTCP, 1500}

	// 16 arbitrary bytes.
	key := []byte{
		0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88,
		0x02, 0xff, 0xe2, 0xd2, 0x4c, 0xce, 0x4f, 0x49,
	}

	client, err := NewConn(clientTCP, core.ClientSide, rp, key, clientProtected)
	if err != nil {
		panic(fmt.Sprintf("Unexpected error creating test ALTS record connection: %v", err))
	}
	server, err := NewConn(serverTCP, core.ServerSide, rp, key, serverProtected)
	if err != nil {
		panic(fmt.Sprintf("Unexpected error creating test ALTS record connection: %v", err))
	}

	return client.(*conn), server.(*conn), nil
}

// mtuConn imposes an MTU on writes. It simulates an important quality of real
// network traffic that is lost when using loopback devices. On loopback, even
// large messages (e.g. 512 KiB) when written often arrive at the receiver
// instantaneously as a single payload. By explicitly splitting such writes into
// smaller, MTU-sized paylaods we give the receiver a chance to respond to
// smaller message sizes.
type mtuConn struct {
	net.Conn
	mtu int
}

// Write implements net.Conn.
func (rc *mtuConn) Write(buf []byte) (int, error) {
	var written int
	for len(buf) > 0 {
		n, err := rc.Conn.Write(buf[:min(rc.mtu, len(buf))])
		written += n
		if err != nil {
			return written, err
		}
		buf = buf[n:]
	}
	return written, nil
}

// SyscallConn implements syscall.Conn.
func (rc *mtuConn) SycallConn() (syscall.RawConn, error) {
	return rc.Conn.(syscall.Conn).SyscallConn()
}

func testPingPong(t *testing.T, rp string) {
	clientConn, serverConn := newConnPair(rp, nil, nil)
	clientMsg := []byte("Client Message")
	if n, err := clientConn.Write(clientMsg); n != len(clientMsg) || err != nil {
		t.Fatalf("Client Write() = %v, %v; want %v, <nil>", n, err, len(clientMsg))
	}
	rcvClientMsg := make([]byte, len(clientMsg))
	if n, err := serverConn.Read(rcvClientMsg); n != len(rcvClientMsg) || err != nil {
		t.Fatalf("Server Read() = %v, %v; want %v, <nil>", n, err, len(rcvClientMsg))
	}
	if !reflect.DeepEqual(clientMsg, rcvClientMsg) {
		t.Fatalf("Client Write()/Server Read() = %v, want %v", rcvClientMsg, clientMsg)
	}

	serverMsg := []byte("Server Message")
	if n, err := serverConn.Write(serverMsg); n != len(serverMsg) || err != nil {
		t.Fatalf("Server Write() = %v, %v; want %v, <nil>", n, err, len(serverMsg))
	}
	rcvServerMsg := make([]byte, len(serverMsg))
	if n, err := clientConn.Read(rcvServerMsg); n != len(rcvServerMsg) || err != nil {
		t.Fatalf("Client Read() = %v, %v; want %v, <nil>", n, err, len(rcvServerMsg))
	}
	if !reflect.DeepEqual(serverMsg, rcvServerMsg) {
		t.Fatalf("Server Write()/Client Read() = %v, want %v", rcvServerMsg, serverMsg)
	}
}

func (s) TestPingPong(t *testing.T) {
	for _, rp := range recordProtocols {
		testPingPong(t, rp)
	}
}

func testSmallReadBuffer(t *testing.T, rp string) {
	clientConn, serverConn := newConnPair(rp, nil, nil)
	msg := []byte("Very Important Message")
	if n, err := clientConn.Write(msg); err != nil {
		t.Fatalf("Write() = %v, %v; want %v, <nil>", n, err, len(msg))
	}
	rcvMsg := make([]byte, len(msg))
	n := 2 // Arbitrary index to break rcvMsg in two.
	rcvMsg1 := rcvMsg[:n]
	rcvMsg2 := rcvMsg[n:]
	if n, err := serverConn.Read(rcvMsg1); n != len(rcvMsg1) || err != nil {
		t.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, len(rcvMsg1))
	}
	if n, err := serverConn.Read(rcvMsg2); n != len(rcvMsg2) || err != nil {
		t.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, len(rcvMsg2))
	}
	if !reflect.DeepEqual(msg, rcvMsg) {
		t.Fatalf("Write()/Read() = %v, want %v", rcvMsg, msg)
	}
}

func (s) TestSmallReadBuffer(t *testing.T) {
	for _, rp := range recordProtocols {
		testSmallReadBuffer(t, rp)
	}
}

func testLargeMsg(t *testing.T, rp string) {
	clientConn, serverConn := newConnPair(rp, nil, nil)
	// msgLen is such that the length in the framing is larger than the
	// default size of one frame.
	msgLen := altsRecordDefaultLength - msgTypeFieldSize - clientConn.crypto.EncryptionOverhead() + 1
	msg := make([]byte, msgLen)
	if n, err := clientConn.Write(msg); n != len(msg) || err != nil {
		t.Fatalf("Write() = %v, %v; want %v, <nil>", n, err, len(msg))
	}
	rcvMsg := make([]byte, len(msg))
	if n, err := io.ReadFull(serverConn, rcvMsg); n != len(rcvMsg) || err != nil {
		t.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, len(rcvMsg))
	}
	if !reflect.DeepEqual(msg, rcvMsg) {
		t.Fatalf("Write()/Server Read() = %v, want %v", rcvMsg, msg)
	}
}

func (s) TestLargeMsg(t *testing.T) {
	for _, rp := range recordProtocols {
		testLargeMsg(t, rp)
	}
}

// TestLargeRecord writes a very large ALTS record and verifies that the server
// receives it correctly. The large ALTS record should cause the reader to
// expand it's read buffer to hold the entire record and store the decrypted
// message until the receiver reads all of the bytes.
func (s) TestLargeRecord(t *testing.T) {
	clientConn, serverConn := newConnPair(rekeyRecordProtocol, nil, nil)
	msg := []byte(strings.Repeat("a", 2*altsReadBufferInitialSize))
	// Increase the size of ALTS records written by the client.
	clientConn.payloadLengthLimit = math.MaxInt32
	if n, err := clientConn.Write(msg); n != len(msg) || err != nil {
		t.Fatalf("Write() = %v, %v; want %v, <nil>", n, err, len(msg))
	}
	rcvMsg := make([]byte, len(msg))
	if n, err := io.ReadFull(serverConn, rcvMsg); n != len(rcvMsg) || err != nil {
		t.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, len(rcvMsg))
	}
	if !reflect.DeepEqual(msg, rcvMsg) {
		t.Fatalf("Write()/Server Read() = %v, want %v", rcvMsg, msg)
	}
}

// BenchmarkLargeMessage measures the performance of ALTS conns for sending and
// receiving a large message.
func BenchmarkLargeMessage(b *testing.B) {
	msgLen := 20 * 1024 * 1024 // 20 MiB
	msg := make([]byte, msgLen)
	rcvMsg := make([]byte, len(msg))
	b.ResetTimer()
	clientConn, serverConn := newConnPair(rekeyRecordProtocol, nil, nil)
	for range b.N {
		// Write 20 MiB 5 times to transfer a total of 100 MiB.
		for range 5 {
			if n, err := clientConn.Write(msg); n != len(msg) || err != nil {
				b.Fatalf("Write() = %v, %v; want %v, <nil>", n, err, len(msg))
			}
			if n, err := io.ReadFull(serverConn, rcvMsg); n != len(rcvMsg) || err != nil {
				b.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, len(rcvMsg))
			}
		}
	}
}

// BenchmarkTCP is a simple throughput test that sends payloads over a local TCP
// connection. Run via:
//
//	go test -run="^$" -bench="BenchmarkTCP" ./credentials/alts/internal/conn
func BenchmarkTCP(b *testing.B) {
	tcs := []struct {
		name string
		size int
	}{
		{"1 KiB", 1024},
		{"4 KiB", 4 * 1024},
		{"64 KiB", 64 * 1024},
		{"512 KiB", 512 * 1024},
		{"1 MiB", 1024 * 1024},
		{"4 MiB", 4 * 1024 * 1024},
	}
	for _, tc := range tcs {
		b.Run("size="+tc.name, func(b *testing.B) {
			benchmarkTCP(b, tc.size)
		})
	}
}

// sum makes unwanted compiler optimizations in benchmarkTCP's loop less likely.
var sum int

func benchmarkTCP(b *testing.B, size int) {
	// Initialize the connection.
	client, server, err := newTCPConnPair(rekeyRecordProtocol, nil, nil)
	if err != nil {
		b.Fatalf("failed to create TCP conn pair: %v", err)
	}
	defer client.Close()
	defer server.Close()

	rcvBuf := make([]byte, size)
	sndBuf := make([]byte, size)
	done := make(chan struct{})
	errChan := make(chan error)

	// Launch a writer goroutine.
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := client.Write(sndBuf)
			if n != size || err != nil {
				errChan <- fmt.Errorf("Write() = %v, %v; want %v, <nil>", n, err, size)
				return
			}
			// Act a bit like a real workload that can't just fill
			// every buffer immediately.
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Get the initial rusage so we can measure CPU time.
	var startUsage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &startUsage); err != nil {
		b.Fatalf("failed to get initial rusage: %v", err)
	}

	// Read as much as possible.
	var rcvd uint64
	for b.Loop() {
		n, err := io.ReadFull(server, rcvBuf)
		rcvd += uint64(n)
		if n != size || err != nil {
			b.Fatalf("Read() = %v, %v; want %v, <nil>", n, err, size)
		}
		// Act a bit like a real workload and utilize received bytes.
		for _, b := range rcvBuf[:n] {
			sum += int(b)
		}
	}

	// Turn off the writer.
	done <- struct{}{}

	// Get the ending rusage.
	var endUsage unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_SELF, &endUsage); err != nil {
		b.Fatalf("failed to get final rusage: %v", err)
	}

	// Error check the writer goroutine.
	select {
	case err := <-errChan:
		b.Fatal(err)
	default:
	}

	// Emit extra metrics.
	utime := timevalDiffUsec(&startUsage.Utime, &endUsage.Utime)
	stime := timevalDiffUsec(&startUsage.Stime, &endUsage.Stime)
	b.ReportMetric(float64(utime)/float64(b.N), "usr-usec/op")
	b.ReportMetric(float64(stime)/float64(b.N), "sys-usec/op")
	b.ReportMetric(float64(stime+utime)/float64(b.N), "cpu-usec/op")
	b.ReportMetric(float64(rcvd*8/(1024*1024))/float64(b.Elapsed().Seconds()), "Mbps")
}

// timevalDiffUsec returns the difference in microseconds between start and end.
func timevalDiffUsec(start, end *unix.Timeval) int64 {
	// Note: the int64 type conversion is needed because unix.Timeval uses
	// 32 bit values on some architectures.
	return int64(1_000_000*(end.Sec-start.Sec) + end.Usec - start.Usec)
}

func testIncorrectMsgType(t *testing.T, rp string) {
	// framedMsg is an empty ciphertext with correct framing but wrong
	// message type.
	framedMsg := make([]byte, MsgLenFieldSize+msgTypeFieldSize)
	binary.LittleEndian.PutUint32(framedMsg[:MsgLenFieldSize], msgTypeFieldSize)
	wrongMsgType := uint32(0x22)
	binary.LittleEndian.PutUint32(framedMsg[MsgLenFieldSize:], wrongMsgType)

	in := bytes.NewBuffer(framedMsg)
	c := newTestALTSRecordConn(in, nil, core.ClientSide, rp, nil)
	b := make([]byte, 1)
	if n, err := c.Read(b); n != 0 || err == nil {
		t.Fatalf("Read() = <nil>, want %v", fmt.Errorf("received frame with incorrect message type %v", wrongMsgType))
	}
}

func (s) TestIncorrectMsgType(t *testing.T) {
	for _, rp := range recordProtocols {
		testIncorrectMsgType(t, rp)
	}
}

func testFrameTooLarge(t *testing.T, rp string) {
	buf := new(bytes.Buffer)
	clientConn := newTestALTSRecordConn(nil, buf, core.ClientSide, rp, nil)
	serverConn := newTestALTSRecordConn(buf, nil, core.ServerSide, rp, nil)
	// payloadLen is such that the length in the framing is larger than
	// allowed in one frame.
	payloadLen := altsRecordLengthLimit - msgTypeFieldSize - clientConn.crypto.EncryptionOverhead() + 1
	payload := make([]byte, payloadLen)
	c, err := clientConn.crypto.Encrypt(nil, payload)
	if err != nil {
		t.Fatalf("Error encrypting message: %v", err)
	}
	msgLen := msgTypeFieldSize + len(c)
	framedMsg := make([]byte, MsgLenFieldSize+msgLen)
	binary.LittleEndian.PutUint32(framedMsg[:MsgLenFieldSize], uint32(msgTypeFieldSize+len(c)))
	msg := framedMsg[MsgLenFieldSize:]
	binary.LittleEndian.PutUint32(msg[:msgTypeFieldSize], altsRecordMsgType)
	copy(msg[msgTypeFieldSize:], c)
	if _, err = buf.Write(framedMsg); err != nil {
		t.Fatalf("Unexpected error writing to buffer: %v", err)
	}
	b := make([]byte, 1)
	if n, err := serverConn.Read(b); n != 0 || err == nil {
		t.Fatalf("Read() = <nil>, want %v", fmt.Errorf("received the frame length %d larger than the limit %d", altsRecordLengthLimit+1, altsRecordLengthLimit))
	}
}

func (s) TestFrameTooLarge(t *testing.T) {
	for _, rp := range recordProtocols {
		testFrameTooLarge(t, rp)
	}
}

func testWriteLargeData(t *testing.T, rp string) {
	// Test sending and receiving messages larger than the maximum write
	// buffer size.
	clientConn, serverConn := newConnPair(rp, nil, nil)
	// Message size is intentionally chosen to not be multiple of
	// payloadLengthLimit.
	msgSize := altsWriteBufferMaxSize + (100 * 1024)
	clientMsg := make([]byte, msgSize)
	for i := 0; i < msgSize; i++ {
		clientMsg[i] = 0xAA
	}
	if n, err := clientConn.Write(clientMsg); n != len(clientMsg) || err != nil {
		t.Fatalf("Client Write() = %v, %v; want %v, <nil>", n, err, len(clientMsg))
	}
	// We need to keep reading until the entire message is received. The
	// reason we set all bytes of the message to a value other than zero is
	// to avoid ambiguous zero-init value of rcvClientMsg buffer and the
	// actual received data.
	rcvClientMsg := make([]byte, 0, msgSize)
	numberOfExpectedFrames := int(math.Ceil(float64(msgSize) / float64(serverConn.payloadLengthLimit)))
	for i := 0; i < numberOfExpectedFrames; i++ {
		expectedRcvSize := serverConn.payloadLengthLimit
		if i == numberOfExpectedFrames-1 {
			// Last frame might be smaller.
			expectedRcvSize = msgSize % serverConn.payloadLengthLimit
		}
		tmpBuf := make([]byte, expectedRcvSize)
		if n, err := serverConn.Read(tmpBuf); n != len(tmpBuf) || err != nil {
			t.Fatalf("Server Read() = %v, %v; want %v, <nil>", n, err, len(tmpBuf))
		}
		rcvClientMsg = append(rcvClientMsg, tmpBuf...)
	}
	if !reflect.DeepEqual(clientMsg, rcvClientMsg) {
		t.Fatalf("Client Write()/Server Read() = %v, want %v", rcvClientMsg, clientMsg)
	}
}

func (s) TestWriteLargeData(t *testing.T) {
	for _, rp := range recordProtocols {
		testWriteLargeData(t, rp)
	}
}

func testProtectedBuffer(t *testing.T, rp string) {
	key := []byte{
		// 16 arbitrary bytes.
		0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xd2, 0x4c, 0xce, 0x4f, 0x49}

	// Encrypt a message to be passed to NewConn as a client-side protected
	// buffer.
	newCrypto := protocols[rp]
	if newCrypto == nil {
		t.Fatalf("Unknown record protocol %q", rp)
	}
	crypto, err := newCrypto(core.ClientSide, key)
	if err != nil {
		t.Fatalf("Failed to create a crypter for protocol %q: %v", rp, err)
	}
	msg := []byte("Client Protected Message")
	encryptedMsg, err := crypto.Encrypt(nil, msg)
	if err != nil {
		t.Fatalf("Failed to encrypt the client protected message: %v", err)
	}
	protectedMsg := make([]byte, 8)                                          // 8 bytes = 4 length + 4 type
	binary.LittleEndian.PutUint32(protectedMsg, uint32(len(encryptedMsg))+4) // 4 bytes for the type
	binary.LittleEndian.PutUint32(protectedMsg[4:], altsRecordMsgType)
	protectedMsg = append(protectedMsg, encryptedMsg...)

	_, serverConn := newConnPair(rp, nil, protectedMsg)
	rcvClientMsg := make([]byte, len(msg))
	if n, err := serverConn.Read(rcvClientMsg); n != len(rcvClientMsg) || err != nil {
		t.Fatalf("Server Read() = %v, %v; want %v, <nil>", n, err, len(rcvClientMsg))
	}
	if !reflect.DeepEqual(msg, rcvClientMsg) {
		t.Fatalf("Client protected/Server Read() = %v, want %v", rcvClientMsg, msg)
	}
}

func (s) TestProtectedBuffer(t *testing.T) {
	for _, rp := range recordProtocols {
		testProtectedBuffer(t, rp)
	}
}
