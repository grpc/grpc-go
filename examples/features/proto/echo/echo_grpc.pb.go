// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package echo

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// EchoClient is the client API for Echo service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type EchoClient interface {
	// UnaryEcho is unary echo.
	UnaryEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (*EchoResponse, error)
	// ServerStreamingEcho is server side streaming.
	ServerStreamingEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (Echo_ServerStreamingEchoClient, error)
	// ClientStreamingEcho is client side streaming.
	ClientStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (Echo_ClientStreamingEchoClient, error)
	// BidirectionalStreamingEcho is bidi streaming.
	BidirectionalStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (Echo_BidirectionalStreamingEchoClient, error)
}

type echoClient struct {
	cc grpc.ClientConnInterface
}

func NewEchoClient(cc grpc.ClientConnInterface) EchoClient {
	return &echoClient{cc}
}

var echoUnaryEchoStreamDesc = &grpc.StreamDesc{
	StreamName: "UnaryEcho",
}

func (c *echoClient) UnaryEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (*EchoResponse, error) {
	out := new(EchoResponse)
	err := c.cc.Invoke(ctx, "/grpc.examples.echo.Echo/UnaryEcho", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var echoServerStreamingEchoStreamDesc = &grpc.StreamDesc{
	StreamName:    "ServerStreamingEcho",
	ServerStreams: true,
}

func (c *echoClient) ServerStreamingEcho(ctx context.Context, in *EchoRequest, opts ...grpc.CallOption) (Echo_ServerStreamingEchoClient, error) {
	stream, err := c.cc.NewStream(ctx, echoServerStreamingEchoStreamDesc, "/grpc.examples.echo.Echo/ServerStreamingEcho", opts...)
	if err != nil {
		return nil, err
	}
	x := &echoServerStreamingEchoClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Echo_ServerStreamingEchoClient interface {
	Recv() (*EchoResponse, error)
	grpc.ClientStream
}

type echoServerStreamingEchoClient struct {
	grpc.ClientStream
}

func (x *echoServerStreamingEchoClient) Recv() (*EchoResponse, error) {
	m := new(EchoResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var echoClientStreamingEchoStreamDesc = &grpc.StreamDesc{
	StreamName:    "ClientStreamingEcho",
	ClientStreams: true,
}

func (c *echoClient) ClientStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (Echo_ClientStreamingEchoClient, error) {
	stream, err := c.cc.NewStream(ctx, echoClientStreamingEchoStreamDesc, "/grpc.examples.echo.Echo/ClientStreamingEcho", opts...)
	if err != nil {
		return nil, err
	}
	x := &echoClientStreamingEchoClient{stream}
	return x, nil
}

type Echo_ClientStreamingEchoClient interface {
	Send(*EchoRequest) error
	CloseAndRecv() (*EchoResponse, error)
	grpc.ClientStream
}

type echoClientStreamingEchoClient struct {
	grpc.ClientStream
}

func (x *echoClientStreamingEchoClient) Send(m *EchoRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *echoClientStreamingEchoClient) CloseAndRecv() (*EchoResponse, error) {
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	m := new(EchoResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var echoBidirectionalStreamingEchoStreamDesc = &grpc.StreamDesc{
	StreamName:    "BidirectionalStreamingEcho",
	ServerStreams: true,
	ClientStreams: true,
}

func (c *echoClient) BidirectionalStreamingEcho(ctx context.Context, opts ...grpc.CallOption) (Echo_BidirectionalStreamingEchoClient, error) {
	stream, err := c.cc.NewStream(ctx, echoBidirectionalStreamingEchoStreamDesc, "/grpc.examples.echo.Echo/BidirectionalStreamingEcho", opts...)
	if err != nil {
		return nil, err
	}
	x := &echoBidirectionalStreamingEchoClient{stream}
	return x, nil
}

type Echo_BidirectionalStreamingEchoClient interface {
	Send(*EchoRequest) error
	Recv() (*EchoResponse, error)
	grpc.ClientStream
}

type echoBidirectionalStreamingEchoClient struct {
	grpc.ClientStream
}

func (x *echoBidirectionalStreamingEchoClient) Send(m *EchoRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *echoBidirectionalStreamingEchoClient) Recv() (*EchoResponse, error) {
	m := new(EchoResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// EchoService is the service API for Echo service.
// Fields should be assigned to their respective handler implementations only before
// RegisterEchoService is called.  Any unassigned fields will result in the
// handler for that method returning an Unimplemented error.
type EchoService struct {
	// UnaryEcho is unary echo.
	UnaryEcho func(context.Context, *EchoRequest) (*EchoResponse, error)
	// ServerStreamingEcho is server side streaming.
	ServerStreamingEcho func(*EchoRequest, Echo_ServerStreamingEchoServer) error
	// ClientStreamingEcho is client side streaming.
	ClientStreamingEcho func(Echo_ClientStreamingEchoServer) error
	// BidirectionalStreamingEcho is bidi streaming.
	BidirectionalStreamingEcho func(Echo_BidirectionalStreamingEchoServer) error
}

func (s *EchoService) unaryEcho(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	if s.UnaryEcho == nil {
		return nil, status.Errorf(codes.Unimplemented, "method UnaryEcho not implemented")
	}
	in := new(EchoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.UnaryEcho(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.examples.echo.Echo/UnaryEcho",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.UnaryEcho(ctx, req.(*EchoRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *EchoService) serverStreamingEcho(_ interface{}, stream grpc.ServerStream) error {
	if s.ServerStreamingEcho == nil {
		return status.Errorf(codes.Unimplemented, "method ServerStreamingEcho not implemented")
	}
	m := new(EchoRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return s.ServerStreamingEcho(m, &echoServerStreamingEchoServer{stream})
}
func (s *EchoService) clientStreamingEcho(_ interface{}, stream grpc.ServerStream) error {
	if s.ClientStreamingEcho == nil {
		return status.Errorf(codes.Unimplemented, "method ClientStreamingEcho not implemented")
	}
	return s.ClientStreamingEcho(&echoClientStreamingEchoServer{stream})
}
func (s *EchoService) bidirectionalStreamingEcho(_ interface{}, stream grpc.ServerStream) error {
	if s.BidirectionalStreamingEcho == nil {
		return status.Errorf(codes.Unimplemented, "method BidirectionalStreamingEcho not implemented")
	}
	return s.BidirectionalStreamingEcho(&echoBidirectionalStreamingEchoServer{stream})
}

type Echo_ServerStreamingEchoServer interface {
	Send(*EchoResponse) error
	grpc.ServerStream
}

type echoServerStreamingEchoServer struct {
	grpc.ServerStream
}

func (x *echoServerStreamingEchoServer) Send(m *EchoResponse) error {
	return x.ServerStream.SendMsg(m)
}

type Echo_ClientStreamingEchoServer interface {
	SendAndClose(*EchoResponse) error
	Recv() (*EchoRequest, error)
	grpc.ServerStream
}

type echoClientStreamingEchoServer struct {
	grpc.ServerStream
}

func (x *echoClientStreamingEchoServer) SendAndClose(m *EchoResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *echoClientStreamingEchoServer) Recv() (*EchoRequest, error) {
	m := new(EchoRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

type Echo_BidirectionalStreamingEchoServer interface {
	Send(*EchoResponse) error
	Recv() (*EchoRequest, error)
	grpc.ServerStream
}

type echoBidirectionalStreamingEchoServer struct {
	grpc.ServerStream
}

func (x *echoBidirectionalStreamingEchoServer) Send(m *EchoResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *echoBidirectionalStreamingEchoServer) Recv() (*EchoRequest, error) {
	m := new(EchoRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RegisterEchoService registers a service implementation with a gRPC server.
func RegisterEchoService(s grpc.ServiceRegistrar, srv *EchoService) {
	sd := grpc.ServiceDesc{
		ServiceName: "grpc.examples.echo.Echo",
		Methods: []grpc.MethodDesc{
			{
				MethodName: "UnaryEcho",
				Handler:    srv.unaryEcho,
			},
		},
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "ServerStreamingEcho",
				Handler:       srv.serverStreamingEcho,
				ServerStreams: true,
			},
			{
				StreamName:    "ClientStreamingEcho",
				Handler:       srv.clientStreamingEcho,
				ClientStreams: true,
			},
			{
				StreamName:    "BidirectionalStreamingEcho",
				Handler:       srv.bidirectionalStreamingEcho,
				ServerStreams: true,
				ClientStreams: true,
			},
		},
		Metadata: "examples/features/proto/echo/echo.proto",
	}

	s.RegisterService(&sd, nil)
}

// NewEchoService creates a new EchoService containing the
// implemented methods of the Echo service in s.  Any unimplemented
// methods will result in the gRPC server returning an UNIMPLEMENTED status to the client.
// This includes situations where the method handler is misspelled or has the wrong
// signature.  For this reason, this function should be used with great care and
// is not recommended to be used by most users.
func NewEchoService(s interface{}) *EchoService {
	ns := &EchoService{}
	if h, ok := s.(interface {
		UnaryEcho(context.Context, *EchoRequest) (*EchoResponse, error)
	}); ok {
		ns.UnaryEcho = h.UnaryEcho
	}
	if h, ok := s.(interface {
		ServerStreamingEcho(*EchoRequest, Echo_ServerStreamingEchoServer) error
	}); ok {
		ns.ServerStreamingEcho = h.ServerStreamingEcho
	}
	if h, ok := s.(interface {
		ClientStreamingEcho(Echo_ClientStreamingEchoServer) error
	}); ok {
		ns.ClientStreamingEcho = h.ClientStreamingEcho
	}
	if h, ok := s.(interface {
		BidirectionalStreamingEcho(Echo_BidirectionalStreamingEchoServer) error
	}); ok {
		ns.BidirectionalStreamingEcho = h.BidirectionalStreamingEcho
	}
	return ns
}

// UnstableEchoService is the service API for Echo service.
// New methods may be added to this interface if they are added to the service
// definition, which is not a backward-compatible change.  For this reason,
// use of this type is not recommended.
type UnstableEchoService interface {
	// UnaryEcho is unary echo.
	UnaryEcho(context.Context, *EchoRequest) (*EchoResponse, error)
	// ServerStreamingEcho is server side streaming.
	ServerStreamingEcho(*EchoRequest, Echo_ServerStreamingEchoServer) error
	// ClientStreamingEcho is client side streaming.
	ClientStreamingEcho(Echo_ClientStreamingEchoServer) error
	// BidirectionalStreamingEcho is bidi streaming.
	BidirectionalStreamingEcho(Echo_BidirectionalStreamingEchoServer) error
}
