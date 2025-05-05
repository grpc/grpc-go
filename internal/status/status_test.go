package status

import (
	"testing"

	"google.golang.org/grpc/codes"
)

func TestRawStatusProto(t *testing.T) {
	spb := RawStatusProto(nil)
	if spb != nil {
		t.Errorf("RawStatusProto(nil) must return nil (was %v)", spb)
	}
	s := New(codes.Internal, "test internal error")
	spb = RawStatusProto(s)
	if spb != s.s {
		t.Errorf("RawStatusProto(s) must return s.s=%p (was %p)", s.s, spb)
	}
}
