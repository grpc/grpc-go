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

	"google.golang.org/grpc/codes"
	rv1pb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Register registers the server reflection service on the given gRPC server.
func RegisterV1(s GRPCServer) {
	svr := NewServerV1(ServerOptions{Services: s})
	rv1pb.RegisterServerReflectionServer(s, svr)
}

// NewServer returns a reflection server implementation using the given options.
// This can be used to customize behavior of the reflection service. Most usages
// should prefer to use Register instead.
//
// # Experimental
//
// Notice: This function is EXPERIMENTAL and may be changed or removed in a
// later release.
func NewServerV1(opts ServerOptions) rv1pb.ServerReflectionServer {
	if opts.DescriptorResolver == nil {
		opts.DescriptorResolver = protoregistry.GlobalFiles
	}
	if opts.ExtensionResolver == nil {
		opts.ExtensionResolver = protoregistry.GlobalTypes
	}
	return &serverReflectionServerV1{
		s:            opts.Services,
		descResolver: opts.DescriptorResolver,
		extResolver:  opts.ExtensionResolver,
	}
}

type serverReflectionServerV1 struct {
	rv1pb.UnimplementedServerReflectionServer
	s            ServiceInfoProvider
	descResolver protodesc.Resolver
	extResolver  ExtensionResolver
}

// fileDescWithDependencies returns a slice of serialized fileDescriptors in
// wire format ([]byte). The fileDescriptors will include fd and all the
// transitive dependencies of fd with names not in sentFileDescriptors.
func (s *serverReflectionServerV1) fileDescWithDependencies(fd protoreflect.FileDescriptor, sentFileDescriptors map[string]bool) ([][]byte, error) {
	var r [][]byte
	queue := []protoreflect.FileDescriptor{fd}
	for len(queue) > 0 {
		currentfd := queue[0]
		queue = queue[1:]
		if sent := sentFileDescriptors[currentfd.Path()]; len(r) == 0 || !sent {
			sentFileDescriptors[currentfd.Path()] = true
			fdProto := protodesc.ToFileDescriptorProto(currentfd)
			currentfdEncoded, err := proto.Marshal(fdProto)
			if err != nil {
				return nil, err
			}
			r = append(r, currentfdEncoded)
		}
		for i := 0; i < currentfd.Imports().Len(); i++ {
			queue = append(queue, currentfd.Imports().Get(i))
		}
	}
	return r, nil
}

// fileDescEncodingContainingSymbol finds the file descriptor containing the
// given symbol, finds all of its previously unsent transitive dependencies,
// does marshalling on them, and returns the marshalled result. The given symbol
// can be a type, a service or a method.
func (s *serverReflectionServerV1) fileDescEncodingContainingSymbol(name string, sentFileDescriptors map[string]bool) ([][]byte, error) {
	d, err := s.descResolver.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	return s.fileDescWithDependencies(d.ParentFile(), sentFileDescriptors)
}

// fileDescEncodingContainingExtension finds the file descriptor containing
// given extension, finds all of its previously unsent transitive dependencies,
// does marshalling on them, and returns the marshalled result.
func (s *serverReflectionServerV1) fileDescEncodingContainingExtension(typeName string, extNum int32, sentFileDescriptors map[string]bool) ([][]byte, error) {
	xt, err := s.extResolver.FindExtensionByNumber(protoreflect.FullName(typeName), protoreflect.FieldNumber(extNum))
	if err != nil {
		return nil, err
	}
	return s.fileDescWithDependencies(xt.TypeDescriptor().ParentFile(), sentFileDescriptors)
}

// allExtensionNumbersForTypeName returns all extension numbers for the given type.
func (s *serverReflectionServerV1) allExtensionNumbersForTypeName(name string) ([]int32, error) {
	var numbers []int32
	s.extResolver.RangeExtensionsByMessage(protoreflect.FullName(name), func(xt protoreflect.ExtensionType) bool {
		numbers = append(numbers, int32(xt.TypeDescriptor().Number()))
		return true
	})
	sort.Slice(numbers, func(i, j int) bool {
		return numbers[i] < numbers[j]
	})
	if len(numbers) == 0 {
		// maybe return an error if given type name is not known
		if _, err := s.descResolver.FindDescriptorByName(protoreflect.FullName(name)); err != nil {
			return nil, err
		}
	}
	return numbers, nil
}

// listServices returns the names of services this server exposes.
func (s *serverReflectionServerV1) listServices() []*rv1pb.ServiceResponse {
	serviceInfo := s.s.GetServiceInfo()
	resp := make([]*rv1pb.ServiceResponse, 0, len(serviceInfo))
	for svc := range serviceInfo {
		resp = append(resp, &rv1pb.ServiceResponse{Name: svc})
	}
	sort.Slice(resp, func(i, j int) bool {
		return resp[i].Name < resp[j].Name
	})
	return resp
}

// ServerReflectionInfo is the reflection service handler.
func (s *serverReflectionServerV1) ServerReflectionInfo(stream rv1pb.ServerReflection_ServerReflectionInfoServer) error {
	sentFileDescriptors := make(map[string]bool)
	for {
		in, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		out := &rv1pb.ServerReflectionResponse{
			ValidHost:       in.Host,
			OriginalRequest: in,
		}
		switch req := in.MessageRequest.(type) {
		case *rv1pb.ServerReflectionRequest_FileByFilename:
			var b [][]byte
			fd, err := s.descResolver.FindFileByPath(req.FileByFilename)
			if err == nil {
				b, err = s.fileDescWithDependencies(fd, sentFileDescriptors)
			}
			if err != nil {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rv1pb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rv1pb.FileDescriptorResponse{FileDescriptorProto: b},
				}
			}
		case *rv1pb.ServerReflectionRequest_FileContainingSymbol:
			b, err := s.fileDescEncodingContainingSymbol(req.FileContainingSymbol, sentFileDescriptors)
			if err != nil {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rv1pb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rv1pb.FileDescriptorResponse{FileDescriptorProto: b},
				}
			}
		case *rv1pb.ServerReflectionRequest_FileContainingExtension:
			typeName := req.FileContainingExtension.ContainingType
			extNum := req.FileContainingExtension.ExtensionNumber
			b, err := s.fileDescEncodingContainingExtension(typeName, extNum, sentFileDescriptors)
			if err != nil {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rv1pb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rv1pb.FileDescriptorResponse{FileDescriptorProto: b},
				}
			}
		case *rv1pb.ServerReflectionRequest_AllExtensionNumbersOfType:
			extNums, err := s.allExtensionNumbersForTypeName(req.AllExtensionNumbersOfType)
			if err != nil {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rv1pb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rv1pb.ServerReflectionResponse_AllExtensionNumbersResponse{
					AllExtensionNumbersResponse: &rv1pb.ExtensionNumberResponse{
						BaseTypeName:    req.AllExtensionNumbersOfType,
						ExtensionNumber: extNums,
					},
				}
			}
		case *rv1pb.ServerReflectionRequest_ListServices:
			out.MessageResponse = &rv1pb.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &rv1pb.ListServiceResponse{
					Service: s.listServices(),
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
