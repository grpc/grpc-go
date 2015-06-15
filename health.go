package grpc

import proto "github.com/golang/protobuf/proto"
import (
	"time"

	"golang.org/x/net/context"
)

type HealthCheckRequest struct {
}

func (m *HealthCheckRequest) Reset()         { *m = HealthCheckRequest{} }
func (m *HealthCheckRequest) String() string { return proto.CompactTextString(m) }
func (*HealthCheckRequest) ProtoMessage()    {}

type HealthCheckResponse struct {
}

func (m *HealthCheckResponse) Reset()         { *m = HealthCheckResponse{} }
func (m *HealthCheckResponse) String() string { return proto.CompactTextString(m) }
func (*HealthCheckResponse) ProtoMessage()    {}

var _HealthCheck_serviceDesc = ServiceDesc{
	ServiceName: "grpc.HealthCheck",
	HandlerType: nil,
	Methods: []MethodDesc{
		{
			MethodName: "Check",
			Handler:    _HealthCheck_Handler,
		},
	},
	Streams: []StreamDesc{},
}

func Enable(s *Server) {
	s.register(&_HealthCheck_serviceDesc, nil)
}

func _HealthCheck_Handler(srv interface{}, ctx context.Context, codec Codec, buf []byte) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := codec.Unmarshal(buf, in); err != nil {
		return nil, err
	}
	out := new(HealthCheckResponse)
	return out, nil
}

func Check(t time.Duration, cc *ClientConn, in *HealthCheckRequest) error {
	ctx, _ := context.WithTimeout(context.Background(), t)
	out := new(HealthCheckResponse)
	if err := Invoke(ctx, "grpc.HealthCheck/Check", in, out, cc); err != nil {
		return err
	}
	return nil
}
