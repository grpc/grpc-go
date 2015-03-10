package grpc

import (
	"github.com/golang/protobuf/proto"
)

type Formatter interface {
	marshal() ([]byte, error)
	unmarshal(buf []byte) error
}

type protoMessage struct {
	proto.Message
}

func (m *protoMessage) marshal() ([]byte, error) {
	if m.Message == nil {
		return nil, nil
	}
	return proto.Marshal(m.Message)
}

func (m *protoMessage) unmarshal(buf []byte) error {
	return proto.Unmarshal(buf, m.Message)
}

func NewProtoMessageFormatter(m proto.Message) Formatter {
	return &protoMessage{m}
}
