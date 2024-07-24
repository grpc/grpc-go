package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/resolver"
)

type hangingClientPreface struct {
	net.Conn
}

func (hc *hangingClientPreface) Write(b []byte) (n int, err error) {
	return 0, errors.New("preface write error")
}

func (hc *hangingClientPreface) Close() error {
	return hc.Conn.Close()
}

func hangingDialerClientPreface(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &hangingClientPreface{Conn: conn}, nil
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
	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: hangingDialerClientPreface}, func(GoAwayReason) {})
	if err == nil {
		t.Error("Expected an error, but got nil")
	}
}

type hangingClientPrefaceLength struct {
	net.Conn
}

func (hc *hangingClientPrefaceLength) Write(b []byte) (n int, err error) {

	incorrectPreface := "INCORRECT PREFACE\r\n\r\n"
	n, err = hc.Conn.Write([]byte(incorrectPreface))
	return n, err
}

func (hc *hangingClientPrefaceLength) Close() error {
	return hc.Conn.Close()
}

func hangingDialerClientPrefaceLength(_ context.Context, addr string) (net.Conn, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &hangingClientPrefaceLength{Conn: conn}, nil
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

	_, err = NewClientTransport(ctx, context.Background(), resolver.Address{Addr: lis.Addr().String()}, ConnectOptions{Dialer: hangingDialerClientPrefaceLength}, func(GoAwayReason) {})
	if err == nil {
		t.Errorf("Expected an error, but got nil")
	}
}
