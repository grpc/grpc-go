// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package grpc_testing

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// TestServiceClient is the client API for TestService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type TestServiceClient interface {
	// One empty request followed by one empty response.
	EmptyCall(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error)
	// One request followed by one response.
	// The server returns the client payload as-is.
	UnaryCall(ctx context.Context, in *SimpleRequest, opts ...grpc.CallOption) (*SimpleResponse, error)
	// One request followed by a sequence of responses (streamed download).
	// The server returns the payload with client desired type and sizes.
	StreamingOutputCall(ctx context.Context, in *StreamingOutputCallRequest, opts ...grpc.CallOption) (TestService_StreamingOutputCallClient, error)
	// A sequence of requests followed by one response (streamed upload).
	// The server returns the aggregated size of client payload as the result.
	StreamingInputCall(ctx context.Context, opts ...grpc.CallOption) (TestService_StreamingInputCallClient, error)
	// A sequence of requests with each request served by the server immediately.
	// As one request could lead to multiple responses, this interface
	// demonstrates the idea of full duplexing.
	FullDuplexCall(ctx context.Context, opts ...grpc.CallOption) (TestService_FullDuplexCallClient, error)
	// A sequence of requests followed by a sequence of responses.
	// The server buffers all the client requests and then serves them in order. A
	// stream of responses are returned to the client when the server starts with
	// first request.
	HalfDuplexCall(ctx context.Context, opts ...grpc.CallOption) (TestService_HalfDuplexCallClient, error)
}

type testServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTestServiceClient(cc grpc.ClientConnInterface) TestServiceClient {
	return &testServiceClient{cc}
}

var testServiceEmptyCallStreamDesc = &grpc.StreamDesc{
	StreamName: "EmptyCall",
}

func (c *testServiceClient) EmptyCall(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error) {
	out := new(Empty)
	err := c.cc.Invoke(ctx, "/grpc.testing.TestService/EmptyCall", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var testServiceUnaryCallStreamDesc = &grpc.StreamDesc{
	StreamName: "UnaryCall",
}

func (c *testServiceClient) UnaryCall(ctx context.Context, in *SimpleRequest, opts ...grpc.CallOption) (*SimpleResponse, error) {
	out := new(SimpleResponse)
	err := c.cc.Invoke(ctx, "/grpc.testing.TestService/UnaryCall", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var testServiceStreamingOutputCallStreamDesc = &grpc.StreamDesc{
	StreamName:    "StreamingOutputCall",
	ServerStreams: true,
}

func (c *testServiceClient) StreamingOutputCall(ctx context.Context, in *StreamingOutputCallRequest, opts ...grpc.CallOption) (TestService_StreamingOutputCallClient, error) {
	stream, err := c.cc.NewStream(ctx, testServiceStreamingOutputCallStreamDesc, "/grpc.testing.TestService/StreamingOutputCall", opts...)
	if err != nil {
		return nil, err
	}
	x := &testServiceStreamingOutputCallClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type TestService_StreamingOutputCallClient interface {
	Recv() (*StreamingOutputCallResponse, error)
	grpc.ClientStream
}

type testServiceStreamingOutputCallClient struct {
	grpc.ClientStream
}

func (x *testServiceStreamingOutputCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var testServiceStreamingInputCallStreamDesc = &grpc.StreamDesc{
	StreamName:    "StreamingInputCall",
	ClientStreams: true,
}

func (c *testServiceClient) StreamingInputCall(ctx context.Context, opts ...grpc.CallOption) (TestService_StreamingInputCallClient, error) {
	stream, err := c.cc.NewStream(ctx, testServiceStreamingInputCallStreamDesc, "/grpc.testing.TestService/StreamingInputCall", opts...)
	if err != nil {
		return nil, err
	}
	x := &testServiceStreamingInputCallClient{stream}
	return x, nil
}

type TestService_StreamingInputCallClient interface {
	Send(*StreamingInputCallRequest) error
	CloseAndRecv() (*StreamingInputCallResponse, error)
	grpc.ClientStream
}

type testServiceStreamingInputCallClient struct {
	grpc.ClientStream
}

func (x *testServiceStreamingInputCallClient) Send(m *StreamingInputCallRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *testServiceStreamingInputCallClient) CloseAndRecv() (*StreamingInputCallResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(StreamingInputCallResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var testServiceFullDuplexCallStreamDesc = &grpc.StreamDesc{
	StreamName:    "FullDuplexCall",
	ServerStreams: true,
	ClientStreams: true,
}

func (c *testServiceClient) FullDuplexCall(ctx context.Context, opts ...grpc.CallOption) (TestService_FullDuplexCallClient, error) {
	stream, err := c.cc.NewStream(ctx, testServiceFullDuplexCallStreamDesc, "/grpc.testing.TestService/FullDuplexCall", opts...)
	if err != nil {
		return nil, err
	}
	x := &testServiceFullDuplexCallClient{stream}
	return x, nil
}

type TestService_FullDuplexCallClient interface {
	Send(*StreamingOutputCallRequest) error
	Recv() (*StreamingOutputCallResponse, error)
	grpc.ClientStream
}

type testServiceFullDuplexCallClient struct {
	grpc.ClientStream
}

func (x *testServiceFullDuplexCallClient) Send(m *StreamingOutputCallRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *testServiceFullDuplexCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var testServiceHalfDuplexCallStreamDesc = &grpc.StreamDesc{
	StreamName:    "HalfDuplexCall",
	ServerStreams: true,
	ClientStreams: true,
}

func (c *testServiceClient) HalfDuplexCall(ctx context.Context, opts ...grpc.CallOption) (TestService_HalfDuplexCallClient, error) {
	stream, err := c.cc.NewStream(ctx, testServiceHalfDuplexCallStreamDesc, "/grpc.testing.TestService/HalfDuplexCall", opts...)
	if err != nil {
		return nil, err
	}
	x := &testServiceHalfDuplexCallClient{stream}
	return x, nil
}

type TestService_HalfDuplexCallClient interface {
	Send(*StreamingOutputCallRequest) error
	Recv() (*StreamingOutputCallResponse, error)
	grpc.ClientStream
}

type testServiceHalfDuplexCallClient struct {
	grpc.ClientStream
}

func (x *testServiceHalfDuplexCallClient) Send(m *StreamingOutputCallRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *testServiceHalfDuplexCallClient) Recv() (*StreamingOutputCallResponse, error) {
	m := new(StreamingOutputCallResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// TestServiceService is the service API for TestService service.
// Fields should be assigned to their respective handler implementations only before
// RegisterTestServiceService is called.  Any unassigned fields will result in the
// handler for that method returning an Unimplemented error.
type TestServiceService struct {
	// One empty request followed by one empty response.
	EmptyCall func(context.Context, *Empty) (*Empty, error)
	// One request followed by one response.
	// The server returns the client payload as-is.
	UnaryCall func(context.Context, *SimpleRequest) (*SimpleResponse, error)
	// One request followed by a sequence of responses (streamed download).
	// The server returns the payload with client desired type and sizes.
	StreamingOutputCall func(*StreamingOutputCallRequest, TestService_StreamingOutputCallServer) error
	// A sequence of requests followed by one response (streamed upload).
	// The server returns the aggregated size of client payload as the result.
	StreamingInputCall func(TestService_StreamingInputCallServer) error
	// A sequence of requests with each request served by the server immediately.
	// As one request could lead to multiple responses, this interface
	// demonstrates the idea of full duplexing.
	FullDuplexCall func(TestService_FullDuplexCallServer) error
	// A sequence of requests followed by a sequence of responses.
	// The server buffers all the client requests and then serves them in order. A
	// stream of responses are returned to the client when the server starts with
	// first request.
	HalfDuplexCall func(TestService_HalfDuplexCallServer) error
}

func (s *TestServiceService) emptyCall(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.EmptyCall(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.testing.TestService/EmptyCall",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.EmptyCall(ctx, req.(*Empty))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *TestServiceService) unaryCall(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SimpleRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.UnaryCall(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.testing.TestService/UnaryCall",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.UnaryCall(ctx, req.(*SimpleRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *TestServiceService) streamingOutputCall(_ interface{}, stream grpc.ServerStream) error {
	m := new(StreamingOutputCallRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return s.StreamingOutputCall(m, &testServiceStreamingOutputCallServer{stream})
}
func (s *TestServiceService) streamingInputCall(_ interface{}, stream grpc.ServerStream) error {
	return s.StreamingInputCall(&testServiceStreamingInputCallServer{stream})
}
func (s *TestServiceService) fullDuplexCall(_ interface{}, stream grpc.ServerStream) error {
	return s.FullDuplexCall(&testServiceFullDuplexCallServer{stream})
}
func (s *TestServiceService) halfDuplexCall(_ interface{}, stream grpc.ServerStream) error {
	return s.HalfDuplexCall(&testServiceHalfDuplexCallServer{stream})
}

type TestService_StreamingOutputCallServer interface {
	Send(*StreamingOutputCallResponse) error
	grpc.ServerStream
}

type testServiceStreamingOutputCallServer struct {
	grpc.ServerStream
}

func (x *testServiceStreamingOutputCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.ServerStream.SendMsg(m)
}

type TestService_StreamingInputCallServer interface {
	SendAndClose(*StreamingInputCallResponse) error
	Recv() (*StreamingInputCallRequest, error)
	grpc.ServerStream
}

type testServiceStreamingInputCallServer struct {
	grpc.ServerStream
}

func (x *testServiceStreamingInputCallServer) SendAndClose(m *StreamingInputCallResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *testServiceStreamingInputCallServer) Recv() (*StreamingInputCallRequest, error) {
	m := new(StreamingInputCallRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type TestService_FullDuplexCallServer interface {
	Send(*StreamingOutputCallResponse) error
	Recv() (*StreamingOutputCallRequest, error)
	grpc.ServerStream
}

type testServiceFullDuplexCallServer struct {
	grpc.ServerStream
}

func (x *testServiceFullDuplexCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *testServiceFullDuplexCallServer) Recv() (*StreamingOutputCallRequest, error) {
	m := new(StreamingOutputCallRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type TestService_HalfDuplexCallServer interface {
	Send(*StreamingOutputCallResponse) error
	Recv() (*StreamingOutputCallRequest, error)
	grpc.ServerStream
}

type testServiceHalfDuplexCallServer struct {
	grpc.ServerStream
}

func (x *testServiceHalfDuplexCallServer) Send(m *StreamingOutputCallResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *testServiceHalfDuplexCallServer) Recv() (*StreamingOutputCallRequest, error) {
	m := new(StreamingOutputCallRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RegisterTestServiceService registers a service implementation with a gRPC server.
func RegisterTestServiceService(s grpc.ServiceRegistrar, srv *TestServiceService) {
	srvCopy := *srv
	if srvCopy.EmptyCall == nil {
		srvCopy.EmptyCall = func(context.Context, *Empty) (*Empty, error) {
			return nil, status.Errorf(codes.Unimplemented, "method EmptyCall not implemented")
		}
	}
	if srvCopy.UnaryCall == nil {
		srvCopy.UnaryCall = func(context.Context, *SimpleRequest) (*SimpleResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method UnaryCall not implemented")
		}
	}
	if srvCopy.StreamingOutputCall == nil {
		srvCopy.StreamingOutputCall = func(*StreamingOutputCallRequest, TestService_StreamingOutputCallServer) error {
			return status.Errorf(codes.Unimplemented, "method StreamingOutputCall not implemented")
		}
	}
	if srvCopy.StreamingInputCall == nil {
		srvCopy.StreamingInputCall = func(TestService_StreamingInputCallServer) error {
			return status.Errorf(codes.Unimplemented, "method StreamingInputCall not implemented")
		}
	}
	if srvCopy.FullDuplexCall == nil {
		srvCopy.FullDuplexCall = func(TestService_FullDuplexCallServer) error {
			return status.Errorf(codes.Unimplemented, "method FullDuplexCall not implemented")
		}
	}
	if srvCopy.HalfDuplexCall == nil {
		srvCopy.HalfDuplexCall = func(TestService_HalfDuplexCallServer) error {
			return status.Errorf(codes.Unimplemented, "method HalfDuplexCall not implemented")
		}
	}
	sd := grpc.ServiceDesc{
		ServiceName: "grpc.testing.TestService",
		Methods: []grpc.MethodDesc{
			{
				MethodName: "EmptyCall",
				Handler:    srvCopy.emptyCall,
			},
			{
				MethodName: "UnaryCall",
				Handler:    srvCopy.unaryCall,
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "StreamingOutputCall",
				Handler:       srvCopy.streamingOutputCall,
				ServerStreams: true,
			},
			{
				StreamName:    "StreamingInputCall",
				Handler:       srvCopy.streamingInputCall,
				ClientStreams: true,
			},
			{
				StreamName:    "FullDuplexCall",
				Handler:       srvCopy.fullDuplexCall,
				ServerStreams: true,
				ClientStreams: true,
			},
			{
				StreamName:    "HalfDuplexCall",
				Handler:       srvCopy.halfDuplexCall,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
		Metadata: "interop/grpc_testing/test.proto",
	}

	s.RegisterService(&sd, nil)
}

// NewTestServiceService creates a new TestServiceService containing the
// implemented methods of the TestService service in s.  Any unimplemented
// methods will result in the gRPC server returning an UNIMPLEMENTED status to the client.
// This includes situations where the method handler is misspelled or has the wrong
// signature.  For this reason, this function should be used with great care and
// is not recommended to be used by most users.
func NewTestServiceService(s interface{}) *TestServiceService {
	ns := &TestServiceService{}
	if h, ok := s.(interface {
		EmptyCall(context.Context, *Empty) (*Empty, error)
	}); ok {
		ns.EmptyCall = h.EmptyCall
	}
	if h, ok := s.(interface {
		UnaryCall(context.Context, *SimpleRequest) (*SimpleResponse, error)
	}); ok {
		ns.UnaryCall = h.UnaryCall
	}
	if h, ok := s.(interface {
		StreamingOutputCall(*StreamingOutputCallRequest, TestService_StreamingOutputCallServer) error
	}); ok {
		ns.StreamingOutputCall = h.StreamingOutputCall
	}
	if h, ok := s.(interface {
		StreamingInputCall(TestService_StreamingInputCallServer) error
	}); ok {
		ns.StreamingInputCall = h.StreamingInputCall
	}
	if h, ok := s.(interface {
		FullDuplexCall(TestService_FullDuplexCallServer) error
	}); ok {
		ns.FullDuplexCall = h.FullDuplexCall
	}
	if h, ok := s.(interface {
		HalfDuplexCall(TestService_HalfDuplexCallServer) error
	}); ok {
		ns.HalfDuplexCall = h.HalfDuplexCall
	}
	return ns
}

// UnstableTestServiceService is the service API for TestService service.
// New methods may be added to this interface if they are added to the service
// definition, which is not a backward-compatible change.  For this reason,
// use of this type is not recommended.
type UnstableTestServiceService interface {
	// One empty request followed by one empty response.
	EmptyCall(context.Context, *Empty) (*Empty, error)
	// One request followed by one response.
	// The server returns the client payload as-is.
	UnaryCall(context.Context, *SimpleRequest) (*SimpleResponse, error)
	// One request followed by a sequence of responses (streamed download).
	// The server returns the payload with client desired type and sizes.
	StreamingOutputCall(*StreamingOutputCallRequest, TestService_StreamingOutputCallServer) error
	// A sequence of requests followed by one response (streamed upload).
	// The server returns the aggregated size of client payload as the result.
	StreamingInputCall(TestService_StreamingInputCallServer) error
	// A sequence of requests with each request served by the server immediately.
	// As one request could lead to multiple responses, this interface
	// demonstrates the idea of full duplexing.
	FullDuplexCall(TestService_FullDuplexCallServer) error
	// A sequence of requests followed by a sequence of responses.
	// The server buffers all the client requests and then serves them in order. A
	// stream of responses are returned to the client when the server starts with
	// first request.
	HalfDuplexCall(TestService_HalfDuplexCallServer) error
}

// UnimplementedServiceClient is the client API for UnimplementedService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type UnimplementedServiceClient interface {
	// A call that no server should implement
	UnimplementedCall(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error)
}

type unimplementedServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewUnimplementedServiceClient(cc grpc.ClientConnInterface) UnimplementedServiceClient {
	return &unimplementedServiceClient{cc}
}

var unimplementedServiceUnimplementedCallStreamDesc = &grpc.StreamDesc{
	StreamName: "UnimplementedCall",
}

func (c *unimplementedServiceClient) UnimplementedCall(ctx context.Context, in *Empty, opts ...grpc.CallOption) (*Empty, error) {
	out := new(Empty)
	err := c.cc.Invoke(ctx, "/grpc.testing.UnimplementedService/UnimplementedCall", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UnimplementedServiceService is the service API for UnimplementedService service.
// Fields should be assigned to their respective handler implementations only before
// RegisterUnimplementedServiceService is called.  Any unassigned fields will result in the
// handler for that method returning an Unimplemented error.
type UnimplementedServiceService struct {
	// A call that no server should implement
	UnimplementedCall func(context.Context, *Empty) (*Empty, error)
}

func (s *UnimplementedServiceService) unimplementedCall(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.UnimplementedCall(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.testing.UnimplementedService/UnimplementedCall",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.UnimplementedCall(ctx, req.(*Empty))
	}
	return interceptor(ctx, in, info, handler)
}

// RegisterUnimplementedServiceService registers a service implementation with a gRPC server.
func RegisterUnimplementedServiceService(s grpc.ServiceRegistrar, srv *UnimplementedServiceService) {
	srvCopy := *srv
	if srvCopy.UnimplementedCall == nil {
		srvCopy.UnimplementedCall = func(context.Context, *Empty) (*Empty, error) {
			return nil, status.Errorf(codes.Unimplemented, "method UnimplementedCall not implemented")
		}
	}
	sd := grpc.ServiceDesc{
		ServiceName: "grpc.testing.UnimplementedService",
		Methods: []grpc.MethodDesc{
			{
				MethodName: "UnimplementedCall",
				Handler:    srvCopy.unimplementedCall,
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "interop/grpc_testing/test.proto",
	}

	s.RegisterService(&sd, nil)
}

// NewUnimplementedServiceService creates a new UnimplementedServiceService containing the
// implemented methods of the UnimplementedService service in s.  Any unimplemented
// methods will result in the gRPC server returning an UNIMPLEMENTED status to the client.
// This includes situations where the method handler is misspelled or has the wrong
// signature.  For this reason, this function should be used with great care and
// is not recommended to be used by most users.
func NewUnimplementedServiceService(s interface{}) *UnimplementedServiceService {
	ns := &UnimplementedServiceService{}
	if h, ok := s.(interface {
		UnimplementedCall(context.Context, *Empty) (*Empty, error)
	}); ok {
		ns.UnimplementedCall = h.UnimplementedCall
	}
	return ns
}

// UnstableUnimplementedServiceService is the service API for UnimplementedService service.
// New methods may be added to this interface if they are added to the service
// definition, which is not a backward-compatible change.  For this reason,
// use of this type is not recommended.
type UnstableUnimplementedServiceService interface {
	// A call that no server should implement
	UnimplementedCall(context.Context, *Empty) (*Empty, error)
}

// LoadBalancerStatsServiceClient is the client API for LoadBalancerStatsService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type LoadBalancerStatsServiceClient interface {
	// Gets the backend distribution for RPCs sent by a test client.
	GetClientStats(ctx context.Context, in *LoadBalancerStatsRequest, opts ...grpc.CallOption) (*LoadBalancerStatsResponse, error)
}

type loadBalancerStatsServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewLoadBalancerStatsServiceClient(cc grpc.ClientConnInterface) LoadBalancerStatsServiceClient {
	return &loadBalancerStatsServiceClient{cc}
}

var loadBalancerStatsServiceGetClientStatsStreamDesc = &grpc.StreamDesc{
	StreamName: "GetClientStats",
}

func (c *loadBalancerStatsServiceClient) GetClientStats(ctx context.Context, in *LoadBalancerStatsRequest, opts ...grpc.CallOption) (*LoadBalancerStatsResponse, error) {
	out := new(LoadBalancerStatsResponse)
	err := c.cc.Invoke(ctx, "/grpc.testing.LoadBalancerStatsService/GetClientStats", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LoadBalancerStatsServiceService is the service API for LoadBalancerStatsService service.
// Fields should be assigned to their respective handler implementations only before
// RegisterLoadBalancerStatsServiceService is called.  Any unassigned fields will result in the
// handler for that method returning an Unimplemented error.
type LoadBalancerStatsServiceService struct {
	// Gets the backend distribution for RPCs sent by a test client.
	GetClientStats func(context.Context, *LoadBalancerStatsRequest) (*LoadBalancerStatsResponse, error)
}

func (s *LoadBalancerStatsServiceService) getClientStats(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LoadBalancerStatsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetClientStats(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.testing.LoadBalancerStatsService/GetClientStats",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetClientStats(ctx, req.(*LoadBalancerStatsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// RegisterLoadBalancerStatsServiceService registers a service implementation with a gRPC server.
func RegisterLoadBalancerStatsServiceService(s grpc.ServiceRegistrar, srv *LoadBalancerStatsServiceService) {
	srvCopy := *srv
	if srvCopy.GetClientStats == nil {
		srvCopy.GetClientStats = func(context.Context, *LoadBalancerStatsRequest) (*LoadBalancerStatsResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetClientStats not implemented")
		}
	}
	sd := grpc.ServiceDesc{
		ServiceName: "grpc.testing.LoadBalancerStatsService",
		Methods: []grpc.MethodDesc{
			{
				MethodName: "GetClientStats",
				Handler:    srvCopy.getClientStats,
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "interop/grpc_testing/test.proto",
	}

	s.RegisterService(&sd, nil)
}

// NewLoadBalancerStatsServiceService creates a new LoadBalancerStatsServiceService containing the
// implemented methods of the LoadBalancerStatsService service in s.  Any unimplemented
// methods will result in the gRPC server returning an UNIMPLEMENTED status to the client.
// This includes situations where the method handler is misspelled or has the wrong
// signature.  For this reason, this function should be used with great care and
// is not recommended to be used by most users.
func NewLoadBalancerStatsServiceService(s interface{}) *LoadBalancerStatsServiceService {
	ns := &LoadBalancerStatsServiceService{}
	if h, ok := s.(interface {
		GetClientStats(context.Context, *LoadBalancerStatsRequest) (*LoadBalancerStatsResponse, error)
	}); ok {
		ns.GetClientStats = h.GetClientStats
	}
	return ns
}

// UnstableLoadBalancerStatsServiceService is the service API for LoadBalancerStatsService service.
// New methods may be added to this interface if they are added to the service
// definition, which is not a backward-compatible change.  For this reason,
// use of this type is not recommended.
type UnstableLoadBalancerStatsServiceService interface {
	// Gets the backend distribution for RPCs sent by a test client.
	GetClientStats(context.Context, *LoadBalancerStatsRequest) (*LoadBalancerStatsResponse, error)
}
