package metadata_test

import (
	"testing"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/grpc_testing"
)

func TestProtos(t *testing.T) {
	msg := grpc_testing.Payload{
		Body: []byte{1, 2, 3, 4, 5},
		Type: grpc_testing.PayloadType_RANDOM.Enum(),
	}
	// adding to existing metadata
	base := metadata.Pairs("key1", "value1", "key2", "value2")
	md1, err := metadata.WithProto(base, &msg)
	if err != nil {
		t.Fatalf("Failed to add proto to metadata: %s", err.Error())
	}
	if len(md1) != 3 || len(md1) != len(base)+1 {
		t.Error("metadata.WithProto did not produce metadata of expected size")
	}
	// adding to nil metadata
	md2, err := metadata.WithProto(nil, &msg)
	if err != nil {
		t.Fatalf("Failed to add proto to metadata: %s", err.Error())
	}
	if len(md2) != 1 {
		t.Error("metadata.WithProto did not produce metadata of expected size")
	}

	// now query back out of both metadatas produced above
	var msg2 grpc_testing.Payload
	ok, err := metadata.GetProto(md1, &msg2)
	if err != nil {
		t.Fatalf("Failed to get proto from metadata: %s", err.Error())
	}
	if !ok {
		t.Fatal("GetProto should have found proto in metadata but did not")
	}
	if !proto.Equal(&msg, &msg2) {
		t.Errorf("Message did not correctly survive round-trip through metadata: %v != %v", &msg, &msg2)
	}

	var msg3 grpc_testing.Payload
	ok, err = metadata.GetProto(md2, &msg3)
	if err != nil {
		t.Fatalf("Failed to get proto from metadata: %s", err.Error())
	}
	if !ok {
		t.Fatal("GetProto should have found proto in metadata but did not")
	}
	if !proto.Equal(&msg, &msg3) {
		t.Errorf("Message did not correctly survive round-trip through metadata: %v != %v", &msg, &msg3)
	}

	// query non-existent proto
	var missing grpc_testing.SimpleRequest
	ok, err = metadata.GetProto(base, &missing)
	if err != nil {
		t.Fatalf("Failed to get proto from metadata: %s", err.Error())
	}
	if ok {
		t.Error("GetProto could not have found proto in metadata but thinks it did")
	}
}
