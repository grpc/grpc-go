// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.1.0
// - protoc             v3.14.0
// source: istio/google/security/meshca/v1/meshca.proto

package google_security_meshca_v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// MeshCertificateServiceClient is the client API for MeshCertificateService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type MeshCertificateServiceClient interface {
	// Using provided CSR, returns a signed certificate that represents a GCP
	// service account identity.
	CreateCertificate(ctx context.Context, in *MeshCertificateRequest, opts ...grpc.CallOption) (*MeshCertificateResponse, error)
}

type meshCertificateServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewMeshCertificateServiceClient(cc grpc.ClientConnInterface) MeshCertificateServiceClient {
	return &meshCertificateServiceClient{cc}
}

func (c *meshCertificateServiceClient) CreateCertificate(ctx context.Context, in *MeshCertificateRequest, opts ...grpc.CallOption) (*MeshCertificateResponse, error) {
	out := new(MeshCertificateResponse)
	err := c.cc.Invoke(ctx, "/google.security.meshca.v1.MeshCertificateService/CreateCertificate", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MeshCertificateServiceServer is the server API for MeshCertificateService service.
// All implementations must embed UnimplementedMeshCertificateServiceServer
// for forward compatibility
type MeshCertificateServiceServer interface {
	// Using provided CSR, returns a signed certificate that represents a GCP
	// service account identity.
	CreateCertificate(context.Context, *MeshCertificateRequest) (*MeshCertificateResponse, error)
	mustEmbedUnimplementedMeshCertificateServiceServer()
}

// UnimplementedMeshCertificateServiceServer must be embedded to have forward compatible implementations.
type UnimplementedMeshCertificateServiceServer struct {
}

func (UnimplementedMeshCertificateServiceServer) CreateCertificate(context.Context, *MeshCertificateRequest) (*MeshCertificateResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateCertificate not implemented")
}
func (UnimplementedMeshCertificateServiceServer) mustEmbedUnimplementedMeshCertificateServiceServer() {
}

// UnsafeMeshCertificateServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to MeshCertificateServiceServer will
// result in compilation errors.
type UnsafeMeshCertificateServiceServer interface {
	mustEmbedUnimplementedMeshCertificateServiceServer()
}

func RegisterMeshCertificateServiceServer(s grpc.ServiceRegistrar, srv MeshCertificateServiceServer) {
	s.RegisterService(&MeshCertificateService_ServiceDesc, srv)
}

func _MeshCertificateService_CreateCertificate_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MeshCertificateRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MeshCertificateServiceServer).CreateCertificate(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/google.security.meshca.v1.MeshCertificateService/CreateCertificate",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MeshCertificateServiceServer).CreateCertificate(ctx, req.(*MeshCertificateRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// MeshCertificateService_ServiceDesc is the grpc.ServiceDesc for MeshCertificateService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var MeshCertificateService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "google.security.meshca.v1.MeshCertificateService",
	HandlerType: (*MeshCertificateServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateCertificate",
			Handler:    _MeshCertificateService_CreateCertificate_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "istio/google/security/meshca/v1/meshca.proto",
}
