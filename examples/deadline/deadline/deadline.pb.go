// Code generated by protoc-gen-go. DO NOT EDIT.
// source: deadline.proto

package deadline

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// The request message
type DeadlinerRequest struct {
	Message              string   `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	Hops                 uint32   `protobuf:"varint,2,opt,name=hops,proto3" json:"hops,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeadlinerRequest) Reset()         { *m = DeadlinerRequest{} }
func (m *DeadlinerRequest) String() string { return proto.CompactTextString(m) }
func (*DeadlinerRequest) ProtoMessage()    {}
func (*DeadlinerRequest) Descriptor() ([]byte, []int) {
	return fileDescriptor_deadline_578d33b6dbdb6ddb, []int{0}
}
func (m *DeadlinerRequest) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_DeadlinerRequest.Unmarshal(m, b)
}
func (m *DeadlinerRequest) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_DeadlinerRequest.Marshal(b, m, deterministic)
}
func (dst *DeadlinerRequest) XXX_Merge(src proto.Message) {
	xxx_messageInfo_DeadlinerRequest.Merge(dst, src)
}
func (m *DeadlinerRequest) XXX_Size() int {
	return xxx_messageInfo_DeadlinerRequest.Size(m)
}
func (m *DeadlinerRequest) XXX_DiscardUnknown() {
	xxx_messageInfo_DeadlinerRequest.DiscardUnknown(m)
}

var xxx_messageInfo_DeadlinerRequest proto.InternalMessageInfo

func (m *DeadlinerRequest) GetMessage() string {
	if m != nil {
		return m.Message
	}
	return ""
}

func (m *DeadlinerRequest) GetHops() uint32 {
	if m != nil {
		return m.Hops
	}
	return 0
}

// The response message
type DeadlinerReply struct {
	Message              string   `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DeadlinerReply) Reset()         { *m = DeadlinerReply{} }
func (m *DeadlinerReply) String() string { return proto.CompactTextString(m) }
func (*DeadlinerReply) ProtoMessage()    {}
func (*DeadlinerReply) Descriptor() ([]byte, []int) {
	return fileDescriptor_deadline_578d33b6dbdb6ddb, []int{1}
}
func (m *DeadlinerReply) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_DeadlinerReply.Unmarshal(m, b)
}
func (m *DeadlinerReply) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_DeadlinerReply.Marshal(b, m, deterministic)
}
func (dst *DeadlinerReply) XXX_Merge(src proto.Message) {
	xxx_messageInfo_DeadlinerReply.Merge(dst, src)
}
func (m *DeadlinerReply) XXX_Size() int {
	return xxx_messageInfo_DeadlinerReply.Size(m)
}
func (m *DeadlinerReply) XXX_DiscardUnknown() {
	xxx_messageInfo_DeadlinerReply.DiscardUnknown(m)
}

var xxx_messageInfo_DeadlinerReply proto.InternalMessageInfo

func (m *DeadlinerReply) GetMessage() string {
	if m != nil {
		return m.Message
	}
	return ""
}

func init() {
	proto.RegisterType((*DeadlinerRequest)(nil), "deadline.DeadlinerRequest")
	proto.RegisterType((*DeadlinerReply)(nil), "deadline.DeadlinerReply")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// DeadlinerClient is the client API for Deadliner service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type DeadlinerClient interface {
	// Sends a greeting
	MakeRequest(ctx context.Context, in *DeadlinerRequest, opts ...grpc.CallOption) (*DeadlinerReply, error)
}

type deadlinerClient struct {
	cc *grpc.ClientConn
}

func NewDeadlinerClient(cc *grpc.ClientConn) DeadlinerClient {
	return &deadlinerClient{cc}
}

func (c *deadlinerClient) MakeRequest(ctx context.Context, in *DeadlinerRequest, opts ...grpc.CallOption) (*DeadlinerReply, error) {
	out := new(DeadlinerReply)
	err := c.cc.Invoke(ctx, "/deadline.Deadliner/MakeRequest", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeadlinerServer is the server API for Deadliner service.
type DeadlinerServer interface {
	// Sends a greeting
	MakeRequest(context.Context, *DeadlinerRequest) (*DeadlinerReply, error)
}

func RegisterDeadlinerServer(s *grpc.Server, srv DeadlinerServer) {
	s.RegisterService(&_Deadliner_serviceDesc, srv)
}

func _Deadliner_MakeRequest_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeadlinerRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(DeadlinerServer).MakeRequest(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/deadline.Deadliner/MakeRequest",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(DeadlinerServer).MakeRequest(ctx, req.(*DeadlinerRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Deadliner_serviceDesc = grpc.ServiceDesc{
	ServiceName: "deadline.Deadliner",
	HandlerType: (*DeadlinerServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "MakeRequest",
			Handler:    _Deadliner_MakeRequest_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "deadline.proto",
}

func init() { proto.RegisterFile("deadline.proto", fileDescriptor_deadline_578d33b6dbdb6ddb) }

var fileDescriptor_deadline_578d33b6dbdb6ddb = []byte{
	// 144 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0xe2, 0x4b, 0x49, 0x4d, 0x4c,
	0xc9, 0xc9, 0xcc, 0x4b, 0xd5, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0xe2, 0x80, 0xf1, 0x95, 0x1c,
	0xb8, 0x04, 0x5c, 0xa0, 0xec, 0xa2, 0xa0, 0xd4, 0xc2, 0xd2, 0xd4, 0xe2, 0x12, 0x21, 0x09, 0x2e,
	0xf6, 0xdc, 0xd4, 0xe2, 0xe2, 0xc4, 0xf4, 0x54, 0x09, 0x46, 0x05, 0x46, 0x0d, 0xce, 0x20, 0x18,
	0x57, 0x48, 0x88, 0x8b, 0x25, 0x23, 0xbf, 0xa0, 0x58, 0x82, 0x49, 0x81, 0x51, 0x83, 0x37, 0x08,
	0xcc, 0x56, 0xd2, 0xe2, 0xe2, 0x43, 0x32, 0xa1, 0x20, 0xa7, 0x12, 0xb7, 0x7e, 0xa3, 0x20, 0x2e,
	0x4e, 0xb8, 0x5a, 0x21, 0x57, 0x2e, 0x6e, 0xdf, 0xc4, 0xec, 0x54, 0x98, 0xad, 0x52, 0x7a, 0x70,
	0x47, 0xa2, 0xbb, 0x48, 0x4a, 0x02, 0xab, 0x5c, 0x41, 0x4e, 0xa5, 0x12, 0x43, 0x12, 0x1b, 0xd8,
	0x4b, 0xc6, 0x80, 0x00, 0x00, 0x00, 0xff, 0xff, 0xee, 0x18, 0xcc, 0x16, 0xe4, 0x00, 0x00, 0x00,
}
