package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/resolver"
)

type hangingConnSettingsWrite struct {
	net.Conn
}

func (hc *hangingConnSettingsWrite) Read(b []byte) (n int, err error) {
	n, err = hc.Conn.Read(b)
	return n, err
}

func (hc *hangingConnSettingsWrite) Write(b []byte) (n int, err error) {
	//return 0, errors.New("write error")
	incorrectPreface := "INCORRECT PREFACE\r\n\r\n"
	n, err = hc.Conn.Write([]byte(incorrectPreface))
	//fmt.Printf("hangingConn Write %v\n", n)
	// if n == 42 {
	// 	time.Sleep(1 * time.Hour) // sleep after writing last frame i.e. GoAway to simulate network hang at client side
	// }
	return n, err
}

func (hc *hangingConnSettingsWrite) Close() error {
	fmt.Printf("hangingConn Close %v\n", time.Now())
	return hc.Conn.Close()
}

func (hc *hangingConnSettingsWrite) LocalAddr() net.Addr {
	return hc.Conn.LocalAddr()
}

func (hc *hangingConnSettingsWrite) RemoteAddr() net.Addr {
	return hc.Conn.RemoteAddr()
}

func (hc *hangingConnSettingsWrite) SetDeadline(t time.Time) error {
	//return hc.Conn.SetDeadline(time.Now().Add(1 * time.Nanosecond))
	return hc.Conn.SetDeadline(t)
}

func (hc *hangingConnSettingsWrite) SetReadDeadline(t time.Time) error {
	return hc.Conn.SetReadDeadline(t)
}

func (hc *hangingConnSettingsWrite) SetWriteDeadline(t time.Time) error {
	//	return hc.Conn.SetWriteDeadline(time.Now().Add(1 * time.Nanosecond))
	return hc.Conn.SetWriteDeadline(t)
}

func hangingDialerSettingsWrite(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &hangingConnSettingsWrite{Conn: conn}, nil
}

func TestNewHTTP2ClientSettingsWriteFailure(t *testing.T) {
	// Create a server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: hangingDialerSettingsWrite}, func(GoAwayReason) {})
	if err != nil {
		t.Errorf("TestNewHTTP2ClientSettingsWriteFailure(): %v", err)
	}
}

type hangingConnSettingsWriteLength struct {
	net.Conn
}

func (hc *hangingConnSettingsWriteLength) Read(b []byte) (n int, err error) {
	n, err = hc.Conn.Read(b)
	return n, err
}

func (hc *hangingConnSettingsWriteLength) Write(b []byte) (n int, err error) {
	return 0, errors.New("write preface error")
	// n, err = hc.Conn.Write(b)
	// fmt.Printf("hangingConn Write %v\n", n)
	// return n, err
}

func (hc *hangingConnSettingsWriteLength) Close() error {
	fmt.Printf("hangingConn Close %v\n", time.Now())
	return hc.Conn.Close()
}

func (hc *hangingConnSettingsWriteLength) LocalAddr() net.Addr {
	return hc.Conn.LocalAddr()
}

func (hc *hangingConnSettingsWriteLength) RemoteAddr() net.Addr {
	return hc.Conn.RemoteAddr()
}

func (hc *hangingConnSettingsWriteLength) SetDeadline(t time.Time) error {
	//return hc.Conn.SetDeadline(time.Now().Add(1 * time.Nanosecond))
	return hc.Conn.SetDeadline(t)
}

func (hc *hangingConnSettingsWriteLength) SetReadDeadline(t time.Time) error {
	return hc.Conn.SetReadDeadline(t)
}

func (hc *hangingConnSettingsWriteLength) SetWriteDeadline(t time.Time) error {
	//return hc.Conn.SetWriteDeadline(time.Now().Add(1 * time.Nanosecond))
	return hc.Conn.SetWriteDeadline(t)
}

func hangingDialerSettingsWriteLength(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &hangingConnSettingsWriteLength{Conn: conn}, nil
}
func TestNewHTTP2ClientSettingsWriteLengthFailure(t *testing.T) {
	// Create a server.
	lis, err := net.Listen("tcp", "localhost:100")
	if err != nil {
		t.Fatalf("Error while listening: %v", err)
	}
	defer lis.Close()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	fmt.Println(ctx)
	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: hangingDialerSettingsWriteLength}, func(GoAwayReason) {})
	if err != nil {
		t.Errorf("NewHTTP2ClientSettingsWriteLengthFailure(): %v", err)

	}
}
