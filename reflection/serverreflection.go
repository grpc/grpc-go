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
	"errors"
	"io"
	"sort"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// GRPCServer is the interface provided by a gRPC server. It is implemented by
// *grpc.Server, but could also be implemented by other concrete types. It acts
// as a registry, for accumulating the services exposed by the server.
type GRPCServer interface {
	grpc.ServiceRegistrar
	GetServiceInfo() map[string]grpc.ServiceInfo
}

// ExtensionResolver is the interface used to query details about extensions.
// This interface is satisfied by protoregistry.GlobalTypes.
type ExtensionResolver interface {
	protoregistry.ExtensionTypeResolver
	RangeExtensionsByMessage(message protoreflect.FullName, f func(protoreflect.ExtensionType) bool)
}

var _ GRPCServer = (*grpc.Server)(nil)

type serverReflectionServer struct {
	rpb.UnimplementedServerReflectionServer
	s            GRPCServer
	descResolver protodesc.Resolver
	extResolver  ExtensionResolver

	initServiceNames sync.Once
	serviceNames     []string
}

// ServerOptions represents the options used to construct a reflection server.
//
// Either Server or ServiceNames must be populated, but not both. These control
// what services are advertised by the server in the ListServices capability of
// the reflection service. If neither is provided, the returned server can still
// serve descriptors, but it will advertise no service names.
//
// The given DescriptorResolver will be used to resolve symbols and files by
// name. If not present, protoregistry.GlobalFiles will be used. The given
// ExtensionResolver will be used to resolve extensions. If not present,
// protoregistry.GlobalTypes will be used.
type ServerOptions struct {
	// An RPC server, whose exposed services are made available via service
	// reflection.
	Server GRPCServer
	// The list of service names. This should only be populated if Server is
	// nil.
	ServiceNames []string
	// Optional resolver used to load descriptors.
	DescriptorResolver protodesc.Resolver
	// Optional resolver used to query for known extensions.
	ExtensionResolver ExtensionResolver
}

// NewServer returns a reflection server implementation using the given options.
// It returns an error if the given options are invalid.
func NewServer(opts ServerOptions) (rpb.ServerReflectionServer, error) {
	if opts.Server != nil && len(opts.ServiceNames) > 0 {
		return nil, errors.New("options must specify either Server or ServiceNames, not both")
	}
	if opts.DescriptorResolver == nil {
		opts.DescriptorResolver = protoregistry.GlobalFiles
	}
	if opts.ExtensionResolver == nil {
		opts.ExtensionResolver = protoregistry.GlobalTypes
	}
	return &serverReflectionServer{
		s:            opts.Server,
		descResolver: opts.DescriptorResolver,
		extResolver:  opts.ExtensionResolver,
		serviceNames: opts.ServiceNames,
	}, nil
}

// Register registers the server reflection service on the given gRPC server.
func Register(s GRPCServer) {
	svr, err := NewServer(ServerOptions{Server: s})
	if err != nil {
		panic(err) // should not be possible
	}
	rpb.RegisterServerReflectionServer(s, svr)
}

func (s *serverReflectionServer) init() {
	s.initServiceNames.Do(func() {
		if s.s == nil {
			// no need to init; service names were specified at construction
			return
		}
		serviceInfo := s.s.GetServiceInfo()
		s.serviceNames = make([]string, 0, len(serviceInfo))
		for svc := range serviceInfo {
			s.serviceNames = append(s.serviceNames, svc)
		}
		sort.Strings(s.serviceNames)
	})
}

// fileDescWithDependencies returns a slice of serialized fileDescriptors in
// wire format ([]byte). The fileDescriptors will include fd and all the
// transitive dependencies of fd with names not in sentFileDescriptors.
func (s *serverReflectionServer) fileDescWithDependencies(fd protoreflect.FileDescriptor, sentFileDescriptors map[string]struct{}) ([][]byte, error) {
	var r [][]byte
	queue := []protoreflect.FileDescriptor{fd}
	for len(queue) > 0 {
		currentfd := queue[0]
		queue = queue[1:]
		if _, sent := sentFileDescriptors[currentfd.Path()]; len(r) == 0 || !sent {
			sentFileDescriptors[currentfd.Path()] = struct{}{}
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
func (s *serverReflectionServer) fileDescEncodingContainingSymbol(name string, sentFileDescriptors map[string]struct{}) ([][]byte, error) {
	d, err := s.descResolver.FindDescriptorByName(protoreflect.FullName(name))
	if err != nil {
		return nil, err
	}
	return s.fileDescWithDependencies(d.ParentFile(), sentFileDescriptors)
}

// fileDescEncodingContainingExtension finds the file descriptor containing
// given extension, finds all of its previously unsent transitive dependencies,
// does marshalling on them, and returns the marshalled result.
func (s *serverReflectionServer) fileDescEncodingContainingExtension(typeName string, extNum int32, sentFileDescriptors map[string]struct{}) ([][]byte, error) {
	xt, err := s.extResolver.FindExtensionByNumber(protoreflect.FullName(typeName), protoreflect.FieldNumber(extNum))
	if err != nil {
		return nil, err
	}
	return s.fileDescWithDependencies(xt.TypeDescriptor().ParentFile(), sentFileDescriptors)
}

// allExtensionNumbersForTypeName returns all extension numbers for the given type.
func (s *serverReflectionServer) allExtensionNumbersForTypeName(name string) ([]int32, error) {
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

// ServerReflectionInfo is the reflection service handler.
func (s *serverReflectionServer) ServerReflectionInfo(stream rpb.ServerReflection_ServerReflectionInfoServer) error {
	s.init()

	sentFileDescriptors := make(map[string]struct{})
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
			var b [][]byte
			fd, err := s.descResolver.FindFileByPath(req.FileByFilename)
			if err == nil {
				b, err = s.fileDescWithDependencies(fd, sentFileDescriptors)
			}
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: b},
				}
			}
		case *rpb.ServerReflectionRequest_FileContainingSymbol:
			b, err := s.fileDescEncodingContainingSymbol(req.FileContainingSymbol, sentFileDescriptors)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: b},
				}
			}
		case *rpb.ServerReflectionRequest_FileContainingExtension:
			typeName := req.FileContainingExtension.ContainingType
			extNum := req.FileContainingExtension.ExtensionNumber
			b, err := s.fileDescEncodingContainingExtension(typeName, extNum, sentFileDescriptors)
			if err != nil {
				out.MessageResponse = &rpb.ServerReflectionResponse_ErrorResponse{
					ErrorResponse: &rpb.ErrorResponse{
						ErrorCode:    int32(codes.NotFound),
						ErrorMessage: err.Error(),
					},
				}
			} else {
				out.MessageResponse = &rpb.ServerReflectionResponse_FileDescriptorResponse{
					FileDescriptorResponse: &rpb.FileDescriptorResponse{FileDescriptorProto: b},
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
			serviceResponses := make([]*rpb.ServiceResponse, len(s.serviceNames))
			for i, n := range s.serviceNames {
				serviceResponses[i] = &rpb.ServiceResponse{
					Name: n,
				}
			}
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
