package status

import (
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/anypb"

	protoV1 "github.com/golang/protobuf/proto"
)

type s struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, s{})
}

func (s) TestWithDetailsVersionsCompat(t *testing.T) {
	details := []protoreflect.ProtoMessage{ // Create with old ptypes and new tspb
		ptypes.TimestampNow(),
		&timestamppb.Timestamp{},
	}
	status := New(codes.InvalidArgument, "an error")

	if _, err = status.WithDetails(details...); err != nil {
		t.Fatal(err)
	}
}

func (s) TestDetailsVersionsCompat(t *testing.T) {
	details := []protoreflect.ProtoMessage{ // Create with old ptypes and new tspb
		ptypes.TimestampNow(),
		timestamppb.Now(),
	}
	status := New(codes.InvalidArgument, "an error")
	status, err := status.WithDetails(details...)

	if err != nil {
		t.Fatal(err)
	}

	for _, detail := range status.Details() {
		if _, ok := detail.(protoV1.Message); !ok {
			t.Fatalf("detail with type %T is not a protoV1.Message", detail)
		}
		if _, ok := detail.(*timestamppb.Timestamp); !ok {
			t.Fatalf("detail with type %T is not a timestamppb.Timestamp", detail)
		}
	}
}