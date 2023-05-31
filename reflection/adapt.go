package reflection

import (
	v1grpc "google.golang.org/grpc/reflection/grpc_reflection_v1"
	v1pb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	v1alphagrpc "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	v1alphapb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

// asV1Alpha returns an implementation of the v1alpha version of the reflection
// interface that delegates all calls to the given v1 version.
func asV1Alpha(svr v1grpc.ServerReflectionServer) v1alphagrpc.ServerReflectionServer {
	return v1AlphaServerImpl{svr: svr}
}

type v1AlphaServerImpl struct {
	svr v1grpc.ServerReflectionServer
}

func (s v1AlphaServerImpl) ServerReflectionInfo(stream v1alphagrpc.ServerReflection_ServerReflectionInfoServer) error {
	return s.svr.ServerReflectionInfo(v1AlphaServerStreamAdapter{stream})
}

type v1AlphaServerStreamAdapter struct {
	v1alphagrpc.ServerReflection_ServerReflectionInfoServer
}

func (s v1AlphaServerStreamAdapter) Send(response *v1pb.ServerReflectionResponse) error {
	return s.ServerReflection_ServerReflectionInfoServer.Send(v1ToV1AlphaResponse(response))
}

func (s v1AlphaServerStreamAdapter) Recv() (*v1pb.ServerReflectionRequest, error) {
	resp, err := s.ServerReflection_ServerReflectionInfoServer.Recv()
	if err != nil {
		return nil, err
	}
	return v1AlphaToV1Request(resp), nil
}

func v1ToV1AlphaResponse(v1 *v1pb.ServerReflectionResponse) *v1alphapb.ServerReflectionResponse {
	var v1alpha v1alphapb.ServerReflectionResponse
	v1alpha.ValidHost = v1.ValidHost
	if v1.OriginalRequest != nil {
		v1alpha.OriginalRequest = v1ToV1AlphaRequest(v1.OriginalRequest)
	}
	switch mr := v1.MessageResponse.(type) {
	case *v1pb.ServerReflectionResponse_FileDescriptorResponse:
		if mr != nil {
			v1alpha.MessageResponse = &v1alphapb.ServerReflectionResponse_FileDescriptorResponse{
				FileDescriptorResponse: &v1alphapb.FileDescriptorResponse{
					FileDescriptorProto: mr.FileDescriptorResponse.GetFileDescriptorProto(),
				},
			}
		}
	case *v1pb.ServerReflectionResponse_AllExtensionNumbersResponse:
		if mr != nil {
			v1alpha.MessageResponse = &v1alphapb.ServerReflectionResponse_AllExtensionNumbersResponse{
				AllExtensionNumbersResponse: &v1alphapb.ExtensionNumberResponse{
					BaseTypeName:    mr.AllExtensionNumbersResponse.GetBaseTypeName(),
					ExtensionNumber: mr.AllExtensionNumbersResponse.GetExtensionNumber(),
				},
			}
		}
	case *v1pb.ServerReflectionResponse_ListServicesResponse:
		if mr != nil {
			svcs := make([]*v1alphapb.ServiceResponse, len(mr.ListServicesResponse.GetService()))
			for i, svc := range mr.ListServicesResponse.GetService() {
				svcs[i] = &v1alphapb.ServiceResponse{
					Name: svc.GetName(),
				}
			}
			v1alpha.MessageResponse = &v1alphapb.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &v1alphapb.ListServiceResponse{
					Service: svcs,
				},
			}
		}
	case *v1pb.ServerReflectionResponse_ErrorResponse:
		if mr != nil {
			v1alpha.MessageResponse = &v1alphapb.ServerReflectionResponse_ErrorResponse{
				ErrorResponse: &v1alphapb.ErrorResponse{
					ErrorCode:    mr.ErrorResponse.GetErrorCode(),
					ErrorMessage: mr.ErrorResponse.GetErrorMessage(),
				},
			}
		}
	default:
		// no value set
	}
	return &v1alpha
}

func v1AlphaToV1Request(v1alpha *v1alphapb.ServerReflectionRequest) *v1pb.ServerReflectionRequest {
	var v1 v1pb.ServerReflectionRequest
	v1.Host = v1alpha.Host
	switch mr := v1alpha.MessageRequest.(type) {
	case *v1alphapb.ServerReflectionRequest_FileByFilename:
		v1.MessageRequest = &v1pb.ServerReflectionRequest_FileByFilename{
			FileByFilename: mr.FileByFilename,
		}
	case *v1alphapb.ServerReflectionRequest_FileContainingSymbol:
		v1.MessageRequest = &v1pb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: mr.FileContainingSymbol,
		}
	case *v1alphapb.ServerReflectionRequest_FileContainingExtension:
		if mr.FileContainingExtension != nil {
			v1.MessageRequest = &v1pb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &v1pb.ExtensionRequest{
					ContainingType:  mr.FileContainingExtension.GetContainingType(),
					ExtensionNumber: mr.FileContainingExtension.GetExtensionNumber(),
				},
			}
		}
	case *v1alphapb.ServerReflectionRequest_AllExtensionNumbersOfType:
		v1.MessageRequest = &v1pb.ServerReflectionRequest_AllExtensionNumbersOfType{
			AllExtensionNumbersOfType: mr.AllExtensionNumbersOfType,
		}
	case *v1alphapb.ServerReflectionRequest_ListServices:
		v1.MessageRequest = &v1pb.ServerReflectionRequest_ListServices{
			ListServices: mr.ListServices,
		}
	default:
		// no value set
	}
	return &v1
}

func v1ToV1AlphaRequest(v1 *v1pb.ServerReflectionRequest) *v1alphapb.ServerReflectionRequest {
	var v1alpha v1alphapb.ServerReflectionRequest
	v1alpha.Host = v1.Host
	switch mr := v1.MessageRequest.(type) {
	case *v1pb.ServerReflectionRequest_FileByFilename:
		if mr != nil {
			v1alpha.MessageRequest = &v1alphapb.ServerReflectionRequest_FileByFilename{
				FileByFilename: mr.FileByFilename,
			}
		}
	case *v1pb.ServerReflectionRequest_FileContainingSymbol:
		if mr != nil {
			v1alpha.MessageRequest = &v1alphapb.ServerReflectionRequest_FileContainingSymbol{
				FileContainingSymbol: mr.FileContainingSymbol,
			}
		}
	case *v1pb.ServerReflectionRequest_FileContainingExtension:
		if mr != nil {
			v1alpha.MessageRequest = &v1alphapb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &v1alphapb.ExtensionRequest{
					ContainingType:  mr.FileContainingExtension.GetContainingType(),
					ExtensionNumber: mr.FileContainingExtension.GetExtensionNumber(),
				},
			}
		}
	case *v1pb.ServerReflectionRequest_AllExtensionNumbersOfType:
		if mr != nil {
			v1alpha.MessageRequest = &v1alphapb.ServerReflectionRequest_AllExtensionNumbersOfType{
				AllExtensionNumbersOfType: mr.AllExtensionNumbersOfType,
			}
		}
	case *v1pb.ServerReflectionRequest_ListServices:
		if mr != nil {
			v1alpha.MessageRequest = &v1alphapb.ServerReflectionRequest_ListServices{
				ListServices: mr.ListServices,
			}
		}
	default:
		// no value set
	}
	return &v1alpha
}
