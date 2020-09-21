// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package grpc_channelz_v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// ChannelzClient is the client API for Channelz service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ChannelzClient interface {
	// Gets all root channels (i.e. channels the application has directly
	// created). This does not include subchannels nor non-top level channels.
	GetTopChannels(ctx context.Context, in *GetTopChannelsRequest, opts ...grpc.CallOption) (*GetTopChannelsResponse, error)
	// Gets all servers that exist in the process.
	GetServers(ctx context.Context, in *GetServersRequest, opts ...grpc.CallOption) (*GetServersResponse, error)
	// Returns a single Server, or else a NOT_FOUND code.
	GetServer(ctx context.Context, in *GetServerRequest, opts ...grpc.CallOption) (*GetServerResponse, error)
	// Gets all server sockets that exist in the process.
	GetServerSockets(ctx context.Context, in *GetServerSocketsRequest, opts ...grpc.CallOption) (*GetServerSocketsResponse, error)
	// Returns a single Channel, or else a NOT_FOUND code.
	GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*GetChannelResponse, error)
	// Returns a single Subchannel, or else a NOT_FOUND code.
	GetSubchannel(ctx context.Context, in *GetSubchannelRequest, opts ...grpc.CallOption) (*GetSubchannelResponse, error)
	// Returns a single Socket or else a NOT_FOUND code.
	GetSocket(ctx context.Context, in *GetSocketRequest, opts ...grpc.CallOption) (*GetSocketResponse, error)
}

type channelzClient struct {
	cc grpc.ClientConnInterface
}

func NewChannelzClient(cc grpc.ClientConnInterface) ChannelzClient {
	return &channelzClient{cc}
}

var channelzGetTopChannelsStreamDesc = &grpc.StreamDesc{
	StreamName: "GetTopChannels",
}

func (c *channelzClient) GetTopChannels(ctx context.Context, in *GetTopChannelsRequest, opts ...grpc.CallOption) (*GetTopChannelsResponse, error) {
	out := new(GetTopChannelsResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetTopChannels", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetServersStreamDesc = &grpc.StreamDesc{
	StreamName: "GetServers",
}

func (c *channelzClient) GetServers(ctx context.Context, in *GetServersRequest, opts ...grpc.CallOption) (*GetServersResponse, error) {
	out := new(GetServersResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetServers", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetServerStreamDesc = &grpc.StreamDesc{
	StreamName: "GetServer",
}

func (c *channelzClient) GetServer(ctx context.Context, in *GetServerRequest, opts ...grpc.CallOption) (*GetServerResponse, error) {
	out := new(GetServerResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetServer", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetServerSocketsStreamDesc = &grpc.StreamDesc{
	StreamName: "GetServerSockets",
}

func (c *channelzClient) GetServerSockets(ctx context.Context, in *GetServerSocketsRequest, opts ...grpc.CallOption) (*GetServerSocketsResponse, error) {
	out := new(GetServerSocketsResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetServerSockets", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetChannelStreamDesc = &grpc.StreamDesc{
	StreamName: "GetChannel",
}

func (c *channelzClient) GetChannel(ctx context.Context, in *GetChannelRequest, opts ...grpc.CallOption) (*GetChannelResponse, error) {
	out := new(GetChannelResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetChannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetSubchannelStreamDesc = &grpc.StreamDesc{
	StreamName: "GetSubchannel",
}

func (c *channelzClient) GetSubchannel(ctx context.Context, in *GetSubchannelRequest, opts ...grpc.CallOption) (*GetSubchannelResponse, error) {
	out := new(GetSubchannelResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetSubchannel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

var channelzGetSocketStreamDesc = &grpc.StreamDesc{
	StreamName: "GetSocket",
}

func (c *channelzClient) GetSocket(ctx context.Context, in *GetSocketRequest, opts ...grpc.CallOption) (*GetSocketResponse, error) {
	out := new(GetSocketResponse)
	err := c.cc.Invoke(ctx, "/grpc.channelz.v1.Channelz/GetSocket", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ChannelzService is the service API for Channelz service.
// Fields should be assigned to their respective handler implementations only before
// RegisterChannelzService is called.  Any unassigned fields will result in the
// handler for that method returning an Unimplemented error.
type ChannelzService struct {
	// Gets all root channels (i.e. channels the application has directly
	// created). This does not include subchannels nor non-top level channels.
	GetTopChannels func(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error)
	// Gets all servers that exist in the process.
	GetServers func(context.Context, *GetServersRequest) (*GetServersResponse, error)
	// Returns a single Server, or else a NOT_FOUND code.
	GetServer func(context.Context, *GetServerRequest) (*GetServerResponse, error)
	// Gets all server sockets that exist in the process.
	GetServerSockets func(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error)
	// Returns a single Channel, or else a NOT_FOUND code.
	GetChannel func(context.Context, *GetChannelRequest) (*GetChannelResponse, error)
	// Returns a single Subchannel, or else a NOT_FOUND code.
	GetSubchannel func(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error)
	// Returns a single Socket or else a NOT_FOUND code.
	GetSocket func(context.Context, *GetSocketRequest) (*GetSocketResponse, error)
}

func (s *ChannelzService) getTopChannels(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetTopChannelsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetTopChannels(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetTopChannels",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetTopChannels(ctx, req.(*GetTopChannelsRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getServers(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServersRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetServers(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetServers",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetServers(ctx, req.(*GetServersRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getServer(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServerRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetServer(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetServer",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetServer(ctx, req.(*GetServerRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getServerSockets(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetServerSocketsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetServerSockets(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetServerSockets",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetServerSockets(ctx, req.(*GetServerSocketsRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getChannel(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetChannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetChannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetChannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetChannel(ctx, req.(*GetChannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getSubchannel(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSubchannelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetSubchannel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetSubchannel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetSubchannel(ctx, req.(*GetSubchannelRequest))
	}
	return interceptor(ctx, in, info, handler)
}
func (s *ChannelzService) getSocket(_ interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetSocketRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return s.GetSocket(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     s,
		FullMethod: "/grpc.channelz.v1.Channelz/GetSocket",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return s.GetSocket(ctx, req.(*GetSocketRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// RegisterChannelzService registers a service implementation with a gRPC server.
func RegisterChannelzService(s grpc.ServiceRegistrar, srv *ChannelzService) {
	srvCopy := *srv
	if srvCopy.GetTopChannels == nil {
		srvCopy.GetTopChannels = func(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetTopChannels not implemented")
		}
	}
	if srvCopy.GetServers == nil {
		srvCopy.GetServers = func(context.Context, *GetServersRequest) (*GetServersResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetServers not implemented")
		}
	}
	if srvCopy.GetServer == nil {
		srvCopy.GetServer = func(context.Context, *GetServerRequest) (*GetServerResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetServer not implemented")
		}
	}
	if srvCopy.GetServerSockets == nil {
		srvCopy.GetServerSockets = func(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetServerSockets not implemented")
		}
	}
	if srvCopy.GetChannel == nil {
		srvCopy.GetChannel = func(context.Context, *GetChannelRequest) (*GetChannelResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetChannel not implemented")
		}
	}
	if srvCopy.GetSubchannel == nil {
		srvCopy.GetSubchannel = func(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetSubchannel not implemented")
		}
	}
	if srvCopy.GetSocket == nil {
		srvCopy.GetSocket = func(context.Context, *GetSocketRequest) (*GetSocketResponse, error) {
			return nil, status.Errorf(codes.Unimplemented, "method GetSocket not implemented")
		}
	}
	sd := grpc.ServiceDesc{
		ServiceName: "grpc.channelz.v1.Channelz",
		Methods: []grpc.MethodDesc{
			{
				MethodName: "GetTopChannels",
				Handler:    srvCopy.getTopChannels,
			},
			{
				MethodName: "GetServers",
				Handler:    srvCopy.getServers,
			},
			{
				MethodName: "GetServer",
				Handler:    srvCopy.getServer,
			},
			{
				MethodName: "GetServerSockets",
				Handler:    srvCopy.getServerSockets,
			},
			{
				MethodName: "GetChannel",
				Handler:    srvCopy.getChannel,
			},
			{
				MethodName: "GetSubchannel",
				Handler:    srvCopy.getSubchannel,
			},
			{
				MethodName: "GetSocket",
				Handler:    srvCopy.getSocket,
			},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "grpc/channelz/v1/channelz.proto",
	}

	s.RegisterService(&sd, nil)
}

// ChannelzServer is the service API for Channelz service.
// New methods may be added to this interface if they are added to the service
// definition, which is not a backward-compatible change.  For this reason,
// use of this type is not recommended unless you own the service definition.
type ChannelzServer interface {
	// Gets all root channels (i.e. channels the application has directly
	// created). This does not include subchannels nor non-top level channels.
	GetTopChannels(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error)
	// Gets all servers that exist in the process.
	GetServers(context.Context, *GetServersRequest) (*GetServersResponse, error)
	// Returns a single Server, or else a NOT_FOUND code.
	GetServer(context.Context, *GetServerRequest) (*GetServerResponse, error)
	// Gets all server sockets that exist in the process.
	GetServerSockets(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error)
	// Returns a single Channel, or else a NOT_FOUND code.
	GetChannel(context.Context, *GetChannelRequest) (*GetChannelResponse, error)
	// Returns a single Subchannel, or else a NOT_FOUND code.
	GetSubchannel(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error)
	// Returns a single Socket or else a NOT_FOUND code.
	GetSocket(context.Context, *GetSocketRequest) (*GetSocketResponse, error)
}

// UnimplementedChannelzServer can be embedded to have forward compatible implementations of
// ChannelzServer
type UnimplementedChannelzServer struct {
}

func (*UnimplementedChannelzServer) GetTopChannels(context.Context, *GetTopChannelsRequest) (*GetTopChannelsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetTopChannels not implemented")
}
func (*UnimplementedChannelzServer) GetServers(context.Context, *GetServersRequest) (*GetServersResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServers not implemented")
}
func (*UnimplementedChannelzServer) GetServer(context.Context, *GetServerRequest) (*GetServerResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServer not implemented")
}
func (*UnimplementedChannelzServer) GetServerSockets(context.Context, *GetServerSocketsRequest) (*GetServerSocketsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetServerSockets not implemented")
}
func (*UnimplementedChannelzServer) GetChannel(context.Context, *GetChannelRequest) (*GetChannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetChannel not implemented")
}
func (*UnimplementedChannelzServer) GetSubchannel(context.Context, *GetSubchannelRequest) (*GetSubchannelResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSubchannel not implemented")
}
func (*UnimplementedChannelzServer) GetSocket(context.Context, *GetSocketRequest) (*GetSocketResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetSocket not implemented")
}

// RegisterChannelzServer registers a service implementation with a gRPC server.
func RegisterChannelzServer(s grpc.ServiceRegistrar, srv ChannelzServer) {
	str := &ChannelzService{
		GetTopChannels:   srv.GetTopChannels,
		GetServers:       srv.GetServers,
		GetServer:        srv.GetServer,
		GetServerSockets: srv.GetServerSockets,
		GetChannel:       srv.GetChannel,
		GetSubchannel:    srv.GetSubchannel,
		GetSocket:        srv.GetSocket,
	}
	RegisterChannelzService(s, str)
}
