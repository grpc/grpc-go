// Code generated by protoc-gen-go. DO NOT EDIT.
// source: envoy/type/range.proto

package envoy_type

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

// This is a compile-time assertion to ensure that this generated file
// is compatible with the proto package it is being compiled against.
// A compilation error at this line likely means your copy of the
// proto package needs to be updated.
const _ = proto.ProtoPackageIsVersion2 // please upgrade the proto package

// Specifies the int64 start and end of the range using half-open interval semantics [start,
// end).
type Int64Range struct {
	// start of the range (inclusive)
	Start int64 `protobuf:"varint,1,opt,name=start,proto3" json:"start,omitempty"`
	// end of the range (exclusive)
	End                  int64    `protobuf:"varint,2,opt,name=end,proto3" json:"end,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *Int64Range) Reset()         { *m = Int64Range{} }
func (m *Int64Range) String() string { return proto.CompactTextString(m) }
func (*Int64Range) ProtoMessage()    {}
func (*Int64Range) Descriptor() ([]byte, []int) {
	return fileDescriptor_range_eb12e26adca8dde3, []int{0}
}
func (m *Int64Range) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_Int64Range.Unmarshal(m, b)
}
func (m *Int64Range) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_Int64Range.Marshal(b, m, deterministic)
}
func (dst *Int64Range) XXX_Merge(src proto.Message) {
	xxx_messageInfo_Int64Range.Merge(dst, src)
}
func (m *Int64Range) XXX_Size() int {
	return xxx_messageInfo_Int64Range.Size(m)
}
func (m *Int64Range) XXX_DiscardUnknown() {
	xxx_messageInfo_Int64Range.DiscardUnknown(m)
}

var xxx_messageInfo_Int64Range proto.InternalMessageInfo

func (m *Int64Range) GetStart() int64 {
	if m != nil {
		return m.Start
	}
	return 0
}

func (m *Int64Range) GetEnd() int64 {
	if m != nil {
		return m.End
	}
	return 0
}

// Specifies the double start and end of the range using half-open interval semantics [start,
// end).
type DoubleRange struct {
	// start of the range (inclusive)
	Start float64 `protobuf:"fixed64,1,opt,name=start,proto3" json:"start,omitempty"`
	// end of the range (exclusive)
	End                  float64  `protobuf:"fixed64,2,opt,name=end,proto3" json:"end,omitempty"`
	XXX_NoUnkeyedLiteral struct{} `json:"-"`
	XXX_unrecognized     []byte   `json:"-"`
	XXX_sizecache        int32    `json:"-"`
}

func (m *DoubleRange) Reset()         { *m = DoubleRange{} }
func (m *DoubleRange) String() string { return proto.CompactTextString(m) }
func (*DoubleRange) ProtoMessage()    {}
func (*DoubleRange) Descriptor() ([]byte, []int) {
	return fileDescriptor_range_eb12e26adca8dde3, []int{1}
}
func (m *DoubleRange) XXX_Unmarshal(b []byte) error {
	return xxx_messageInfo_DoubleRange.Unmarshal(m, b)
}
func (m *DoubleRange) XXX_Marshal(b []byte, deterministic bool) ([]byte, error) {
	return xxx_messageInfo_DoubleRange.Marshal(b, m, deterministic)
}
func (dst *DoubleRange) XXX_Merge(src proto.Message) {
	xxx_messageInfo_DoubleRange.Merge(dst, src)
}
func (m *DoubleRange) XXX_Size() int {
	return xxx_messageInfo_DoubleRange.Size(m)
}
func (m *DoubleRange) XXX_DiscardUnknown() {
	xxx_messageInfo_DoubleRange.DiscardUnknown(m)
}

var xxx_messageInfo_DoubleRange proto.InternalMessageInfo

func (m *DoubleRange) GetStart() float64 {
	if m != nil {
		return m.Start
	}
	return 0
}

func (m *DoubleRange) GetEnd() float64 {
	if m != nil {
		return m.End
	}
	return 0
}

func init() {
	proto.RegisterType((*Int64Range)(nil), "envoy.type.Int64Range")
	proto.RegisterType((*DoubleRange)(nil), "envoy.type.DoubleRange")
}

func init() { proto.RegisterFile("envoy/type/range.proto", fileDescriptor_range_eb12e26adca8dde3) }

var fileDescriptor_range_eb12e26adca8dde3 = []byte{
	// 154 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0xff, 0xe2, 0x12, 0x4b, 0xcd, 0x2b, 0xcb,
	0xaf, 0xd4, 0x2f, 0xa9, 0x2c, 0x48, 0xd5, 0x2f, 0x4a, 0xcc, 0x4b, 0x4f, 0xd5, 0x2b, 0x28, 0xca,
	0x2f, 0xc9, 0x17, 0xe2, 0x02, 0x8b, 0xeb, 0x81, 0xc4, 0x95, 0x4c, 0xb8, 0xb8, 0x3c, 0xf3, 0x4a,
	0xcc, 0x4c, 0x82, 0x40, 0xf2, 0x42, 0x22, 0x5c, 0xac, 0xc5, 0x25, 0x89, 0x45, 0x25, 0x12, 0x8c,
	0x0a, 0x8c, 0x1a, 0xcc, 0x41, 0x10, 0x8e, 0x90, 0x00, 0x17, 0x73, 0x6a, 0x5e, 0x8a, 0x04, 0x13,
	0x58, 0x0c, 0xc4, 0x54, 0x32, 0xe5, 0xe2, 0x76, 0xc9, 0x2f, 0x4d, 0xca, 0x49, 0xc5, 0xa2, 0x8d,
	0x11, 0x8b, 0x36, 0x46, 0xb0, 0x36, 0x27, 0x13, 0x2e, 0x89, 0xcc, 0x7c, 0x3d, 0xb0, 0xed, 0x05,
	0x45, 0xf9, 0x15, 0x95, 0x7a, 0x08, 0x87, 0x38, 0x71, 0x81, 0x8d, 0x0a, 0x00, 0x39, 0x30, 0x80,
	0x31, 0x0a, 0xe2, 0xc4, 0x78, 0x90, 0x4c, 0x12, 0x1b, 0xd8, 0xd5, 0xc6, 0x80, 0x00, 0x00, 0x00,
	0xff, 0xff, 0x7f, 0xad, 0x02, 0xe6, 0xcf, 0x00, 0x00, 0x00,
}
