package grpchttp2_test

import (
	"testing"

	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/grpc/internal/transport/grpchttp2"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestErrorCodeString(t *testing.T) {
	err := grpchttp2.ErrCodeNoError
	if err.String() != "NO_ERROR" {
		t.Errorf("got %q, want %q", err.String(), "NO_ERROR")
	}

	err = grpchttp2.ErrCode(0x1)
	if err.String() != "PROTOCOL_ERROR" {
		t.Errorf("got %q, want %q", err.String(), "PROTOCOL_ERROR")
	}

	err = grpchttp2.ErrCode(0xf)
	if err.String() != "unknown error code 0xf" {
		t.Errorf("got %q, want %q", err.String(), "unknown error code 0xf")
	}
}
