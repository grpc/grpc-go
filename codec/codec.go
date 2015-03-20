package codec

import (
	"github.com/golang/protobuf/proto"
)

// Codec defines the interface gRPC uses to encode and decode messages.
type Codec interface {
	// Marshal returns the encoded of v.
	Marshal(v interface{}) ([]byte, error)
	// Unmarshal parses the encoded data and stores the result in the value pointed to by v.
	Unmarshal(data []byte, v interface{}) error
	// String returns the name of the Codec implementation.
	String() string
}

// protoCodec is a Codec implemetation with protobuf. It is the default codec for gRPC.
type protoCodec struct{}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

func (protoCodec) String() string {
	return "proto"
}

// NewProtoCodec creates a protobuf Codec.
func NewProtoCodec() Codec {
	return &protoCodec{}
}

// GetCodec chooses a Codec implementation according tothe input content type string ct.
func GetCodec(ct string) Codec {
	switch ct {
	// Add cases here to support custom non-protobuf data formats. e.g.,
	// case: "application/grpc+proto":
	//	return &protoCodec{}
	default:
		return &protoCodec{}
	}
}
