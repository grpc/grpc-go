/*
 *
 * Copyright 2016 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

/*
Package reflection implements server reflection service.

The service implemented is defined in:
https://github.com/grpc/grpc/blob/master/src/proto/grpc/reflection/v1alpha/reflection.proto.

To register server reflection on a gRPC server:
	import "google.golang.org/grpc/reflection"

	s := grpc.NewServer()
	pb.RegisterYourOwnServer(s, &server{})

	// Register reflection service on gRPC server.
	reflection.Register(s)

	s.Serve(lis)

*/
package reflection // import "google.golang.org/grpc/reflection"

import (
	"io"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type serverReflectionServer struct {
	s *grpc.Server
}

// Register registers the server reflection service on the given gRPC server.
func Register(s *grpc.Server) {
	rpb.RegisterServerReflectionServer(s, &serverReflectionServer{
		s: s,
	})
}

// fileDescEncodingByFilename finds the file descriptor for given filename,
// does marshalling on it and returns the marshalled result.
func (s *serverReflectionServer) fileDescEncodingByFilename(name string) ([]byte, error) {
	fd, err := protoregistry.GlobalFiles.FindFileByPath(name)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(protodesc.ToFileDescriptorProto(fd))
}

// fileDescEncodingContainingSymbol finds the file descriptor containing the given symbol,
// does marshalling on it and returns the marshalled result.
// The given symbol can be a type, a service or a method.
func (s *serverReflectionServer) fileDescEncodingContainingSymbol(name string) ([]byte, error) {
	fullname := protoreflect.FullName(name)
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(fullname)
	if err != nil {
		return nil, err
	}
	fd := d.ParentFile()
	return proto.Marshal(protodesc.ToFileDescriptorProto(fd))
}

// fileDescEncodingContainingExtension finds the file descriptor containing given extension,
// does marshalling on it and returns the marshalled result.
func (s *serverReflectionServer) fileDescEncodingContainingExtension(typeName string, extNum int32) ([]byte, error) {
	fullname := protoreflect.FullName(typeName)
	fieldnumber := protoreflect.FieldNumber(extNum)
	ext, err := protoregistry.GlobalTypes.FindExtensionByNumber(fullname, fieldnumber)
	if err != nil {
		return nil, err
	}

	extd := ext.TypeDescriptor()
	d, err := protoregistry.GlobalFiles.FindDescriptorByName(extd.FullName())
	if err != nil {
		return nil, err
	}
	fd := d.ParentFile()

	return proto.Marshal(protodesc.ToFileDescriptorProto(fd))
}

// allExtensionNumbersForTypeName returns all extension numbers for the given type.
func (s *serverReflectionServer) allExtensionNumbersForTypeName(name string) ([]int32, error) {
	fullname := protoreflect.FullName(name)
	_, err := protoregistry.GlobalFiles.FindDescriptorByName(fullname)
	if err != nil {
		return nil, err
	}

	n := protoregistry.GlobalTypes.NumExtensionsByMessage(fullname)
	if n == 0 {
		return nil, nil
	}

	extNums := make([]int32, 0, n)
	protoregistry.GlobalTypes.RangeExtensionsByMessage(
		fullname,
		func(et protoreflect.ExtensionType) bool {
			ed := et.TypeDescriptor().Descriptor()
			extNums = append(extNums, int32(ed.Number()))
			return true
		},
	)
	return extNums, nil
}

// ServerReflectionInfo is the reflection service handler.
func (s *serverReflectionServer) ServerReflectionInfo(stream rpb.ServerReflection_ServerReflectionInfoServer) error {
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		out := &rpb.ServerReflectionResponse{
			ValidHost:       in.Host,
			OriginalRequest: in,
		}
		switch req := in.MessageRequest.(type) {
		case *rpb.ServerReflectionRequest_FileByFilename:
			b, err := s.fileDescEncodingByFilename(req.FileByFilename)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: [][]byte{b}},
				}
			}
		case *rpb.ServerReflectionRequest_FileContainingSymbol:
			b, err := s.fileDescEncodingContainingSymbol(req.FileContainingSymbol)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: [][]byte{b}},
				}
			}
		case *rpb.ServerReflectionRequest_FileContainingExtension:
			typeName := req.FileContainingExtension.ContainingType
			extNum := req.FileContainingExtension.ExtensionNumber
			b, err := s.fileDescEncodingContainingExtension(typeName, extNum)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: [][]byte{b}},
				}
			}
		case *rpb.ServerReflectionRequest_AllExtensionNumbersOfType:
			extNums, err := s.allExtensionNumbersForTypeName(req.AllExtensionNumbersOfType)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_AllExtensionNumbersResponse{
					AllExtensionNumbersResponse: &rpb.ExtensionNumberResponse{
						BaseTypeName:    req.AllExtensionNumbersOfType,
						ExtensionNumber: extNums,
					},
				}
			}
		case *rpb.ServerReflectionRequest_ListServices:
			svcInfo := s.s.GetServiceInfo()
			serviceResponses := make([]*rpb.ServiceResponse, 0, len(svcInfo))
			for svcName := range svcInfo {
				serviceResponses = append(serviceResponses, &rpb.ServiceResponse{
					Name: svcName,
				})
			}
			sort.Slice(serviceResponses, func(i, j int) bool {
				return serviceResponses[i].Name < serviceResponses[j].Name
			})
			out.MessageResponse = &rpb.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &rpb.ListServiceResponse{
					Service: serviceResponses,
				},
			}
		default:
			return status.Errorf(codes.InvalidArgument, "invalid MessageRequest: %v", in.MessageRequest)
		}

		if err := stream.Send(out); err != nil {
			return err
		}
	}
}
