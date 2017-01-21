// Code generated by protoc-gen-go.
// source: test.proto
// DO NOT EDIT!

package grpc_testing

import proto "github.com/golang/protobuf/proto"
import fmt "fmt"
import math "math"

import (
	context "golang.org/x/net/context"
	grpc "github.com/lypnol/grpc-go"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = fmt.Errorf
var _ = math.Inf

type SearchResponse struct {
	Results []*SearchResponse_Result `protobuf:"bytes,1,rep,name=results" json:"results,omitempty"`
}

func (m *SearchResponse) Reset()                    { *m = SearchResponse{} }
func (m *SearchResponse) String() string            { return proto.CompactTextString(m) }
func (*SearchResponse) ProtoMessage()               {}
func (*SearchResponse) Descriptor() ([]byte, []int) { return fileDescriptor2, []int{0} }

func (m *SearchResponse) GetResults() []*SearchResponse_Result {
	if m != nil {
		return m.Results
	}
	return nil
}

type SearchResponse_Result struct {
	Url      string   `protobuf:"bytes,1,opt,name=url" json:"url,omitempty"`
	Title    string   `protobuf:"bytes,2,opt,name=title" json:"title,omitempty"`
	Snippets []string `protobuf:"bytes,3,rep,name=snippets" json:"snippets,omitempty"`
}

func (m *SearchResponse_Result) Reset()                    { *m = SearchResponse_Result{} }
func (m *SearchResponse_Result) String() string            { return proto.CompactTextString(m) }
func (*SearchResponse_Result) ProtoMessage()               {}
func (*SearchResponse_Result) Descriptor() ([]byte, []int) { return fileDescriptor2, []int{0, 0} }

type SearchRequest struct {
	Query string `protobuf:"bytes,1,opt,name=query" json:"query,omitempty"`
}

func (m *SearchRequest) Reset()                    { *m = SearchRequest{} }
func (m *SearchRequest) String() string            { return proto.CompactTextString(m) }
func (*SearchRequest) ProtoMessage()               {}
func (*SearchRequest) Descriptor() ([]byte, []int) { return fileDescriptor2, []int{1} }

func init() {
	proto.RegisterType((*SearchResponse)(nil), "grpc.testing.SearchResponse")
	proto.RegisterType((*SearchResponse_Result)(nil), "grpc.testing.SearchResponse.Result")
	proto.RegisterType((*SearchRequest)(nil), "grpc.testing.SearchRequest")
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// Client API for SearchService service

type SearchServiceClient interface {
	Search(ctx context.Context, in *SearchRequest, opts ...grpc.CallOption) (*SearchResponse, error)
	StreamingSearch(ctx context.Context, opts ...grpc.CallOption) (SearchService_StreamingSearchClient, error)
}

type searchServiceClient struct {
	cc *grpc.ClientConn
}

func NewSearchServiceClient(cc *grpc.ClientConn) SearchServiceClient {
	return &searchServiceClient{cc}
}

func (c *searchServiceClient) Search(ctx context.Context, in *SearchRequest, opts ...grpc.CallOption) (*SearchResponse, error) {
	out := new(SearchResponse)
	err := grpc.Invoke(ctx, "/grpc.testing.SearchService/Search", in, out, c.cc, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *searchServiceClient) StreamingSearch(ctx context.Context, opts ...grpc.CallOption) (SearchService_StreamingSearchClient, error) {
	stream, err := grpc.NewClientStream(ctx, &_SearchService_serviceDesc.Streams[0], c.cc, "/grpc.testing.SearchService/StreamingSearch", opts...)
	if err != nil {
		return nil, err
	}
	x := &searchServiceStreamingSearchClient{stream}
	return x, nil
}

type SearchService_StreamingSearchClient interface {
	Send(*SearchRequest) error
	Recv() (*SearchResponse, error)
	grpc.ClientStream
}

type searchServiceStreamingSearchClient struct {
	grpc.ClientStream
}

func (x *searchServiceStreamingSearchClient) Send(m *SearchRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *searchServiceStreamingSearchClient) Recv() (*SearchResponse, error) {
	m := new(SearchResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Server API for SearchService service

type SearchServiceServer interface {
	Search(context.Context, *SearchRequest) (*SearchResponse, error)
	StreamingSearch(SearchService_StreamingSearchServer) error
}

func RegisterSearchServiceServer(s *grpc.Server, srv SearchServiceServer) {
	s.RegisterService(&_SearchService_serviceDesc, srv)
}

func _SearchService_Search_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SearchRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(SearchServiceServer).Search(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/grpc.testing.SearchService/Search",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(SearchServiceServer).Search(ctx, req.(*SearchRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _SearchService_StreamingSearch_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(SearchServiceServer).StreamingSearch(&searchServiceStreamingSearchServer{stream})
}

type SearchService_StreamingSearchServer interface {
	Send(*SearchResponse) error
	Recv() (*SearchRequest, error)
	grpc.ServerStream
}

type searchServiceStreamingSearchServer struct {
	grpc.ServerStream
}

func (x *searchServiceStreamingSearchServer) Send(m *SearchResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *searchServiceStreamingSearchServer) Recv() (*SearchRequest, error) {
	m := new(SearchRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var _SearchService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "grpc.testing.SearchService",
	HandlerType: (*SearchServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Search",
			Handler:    _SearchService_Search_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "StreamingSearch",
			Handler:       _SearchService_StreamingSearch_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "test.proto",
}

func init() { proto.RegisterFile("test.proto", fileDescriptor2) }

var fileDescriptor2 = []byte{
	// 227 bytes of a gzipped FileDescriptorProto
	0x1f, 0x8b, 0x08, 0x00, 0x00, 0x09, 0x6e, 0x88, 0x02, 0xff, 0xe2, 0xe2, 0x2a, 0x49, 0x2d, 0x2e,
	0xd1, 0x2b, 0x28, 0xca, 0x2f, 0xc9, 0x17, 0xe2, 0x49, 0x2f, 0x2a, 0x48, 0xd6, 0x03, 0x09, 0x64,
	0xe6, 0xa5, 0x2b, 0xcd, 0x65, 0xe4, 0xe2, 0x0b, 0x4e, 0x4d, 0x2c, 0x4a, 0xce, 0x08, 0x4a, 0x2d,
	0x2e, 0xc8, 0xcf, 0x2b, 0x4e, 0x15, 0xb2, 0xe5, 0x62, 0x2f, 0x4a, 0x2d, 0x2e, 0xcd, 0x29, 0x29,
	0x96, 0x60, 0x54, 0x60, 0xd6, 0xe0, 0x36, 0x52, 0xd6, 0x43, 0xd6, 0xa2, 0x87, 0xaa, 0x5c, 0x2f,
	0x08, 0xac, 0x36, 0x08, 0xa6, 0x47, 0xca, 0x87, 0x8b, 0x0d, 0x22, 0x24, 0x24, 0xc0, 0xc5, 0x5c,
	0x5a, 0x94, 0x03, 0x34, 0x84, 0x51, 0x83, 0x33, 0x08, 0xc4, 0x14, 0x12, 0xe1, 0x62, 0x2d, 0xc9,
	0x2c, 0xc9, 0x49, 0x95, 0x60, 0x02, 0x8b, 0x41, 0x38, 0x42, 0x52, 0x5c, 0x1c, 0xc5, 0x79, 0x99,
	0x05, 0x05, 0xa9, 0x40, 0x1b, 0x99, 0x81, 0x36, 0x72, 0x06, 0xc1, 0xf9, 0x4a, 0xaa, 0x5c, 0xbc,
	0x30, 0xfb, 0x0a, 0x4b, 0x81, 0x0e, 0x00, 0x19, 0x01, 0x64, 0x14, 0x55, 0x42, 0x8d, 0x85, 0x70,
	0x8c, 0x96, 0x31, 0xc2, 0xd4, 0x05, 0xa7, 0x16, 0x95, 0x65, 0x26, 0xa7, 0x0a, 0x39, 0x73, 0xb1,
	0x41, 0x04, 0x84, 0xa4, 0xb1, 0x3b, 0x1f, 0x6c, 0x9c, 0x94, 0x0c, 0x3e, 0xbf, 0x09, 0x05, 0x70,
	0xf1, 0x07, 0x97, 0x14, 0xa5, 0x26, 0xe6, 0x02, 0xe5, 0x28, 0x36, 0x4d, 0x83, 0xd1, 0x80, 0x31,
	0x89, 0x0d, 0x1c, 0x09, 0xc6, 0x80, 0x00, 0x00, 0x00, 0xff, 0xff, 0x20, 0xd6, 0x09, 0xb8, 0x92,
	0x01, 0x00, 0x00,
}
