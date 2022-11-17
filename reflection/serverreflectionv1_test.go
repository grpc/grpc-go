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

package reflection

import (
	"context"
	"net"
	"reflect"
	"sort"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	rv1pb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	pb "google.golang.org/grpc/reflection/grpc_testing"
	pbv3 "google.golang.org/grpc/reflection/grpc_testing_not_regenerate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

var (
	sv1 = NewServerV1(ServerOptions{}).(*serverReflectionServerV1)
)

type xv1 struct {
	grpctest.Tester
}

func TestV1(t *testing.T) {
	grpctest.RunSubTests(t, xv1{})
}

// func loadFileDesc(filename string) (*descriptorpb.FileDescriptorProto, []byte) {
// 	fd, err := protoregistry.GlobalFiles.FindFileByPath(filename)
// 	if err != nil {
// 		panic(err)
// 	}
// 	fdProto := protodesc.ToFileDescriptorProto(fd)
// 	b, err := proto.Marshal(fdProto)
// 	if err != nil {
// 		panic(fmt.Sprintf("failed to marshal fd: %v", err))
// 	}
// 	return fdProto, b
// }

// func loadFileDescDynamic(b []byte) (*descriptorpb.FileDescriptorProto, protoreflect.FileDescriptor, []byte) {
// 	m := new(descriptorpb.FileDescriptorProto)
// 	if err := proto.Unmarshal(b, m); err != nil {
// 		panic("failed to unmarshal dynamic proto raw descriptor")
// 	}

// 	fd, err := protodesc.NewFile(m, nil)
// 	if err != nil {
// 		panic(err)
// 	}

// 	err = protoregistry.GlobalFiles.RegisterFile(fd)
// 	if err != nil {
// 		panic(err)
// 	}

// 	for i := 0; i < fd.Messages().Len(); i++ {
// 		m := fd.Messages().Get(i)
// 		if err := protoregistry.GlobalTypes.RegisterMessage(dynamicpb.NewMessageType(m)); err != nil {
// 			panic(err)
// 		}
// 	}

// 	return m, fd, b
// }

func (xv1) TestFileDescContainingExtension(t *testing.T) {
	for _, test := range []struct {
		st     string
		extNum int32
		want   *descriptorpb.FileDescriptorProto
	}{
		{"grpc.testing.ToBeExtended", 13, fdProto2Ext},
		{"grpc.testing.ToBeExtended", 17, fdProto2Ext},
		{"grpc.testing.ToBeExtended", 19, fdProto2Ext},
		{"grpc.testing.ToBeExtended", 23, fdProto2Ext2},
		{"grpc.testing.ToBeExtended", 29, fdProto2Ext2},
	} {
		fd, err := sv1.fileDescEncodingContainingExtension(test.st, test.extNum, map[string]bool{})
		if err != nil {
			t.Errorf("fileDescContainingExtension(%q) return error: %v", test.st, err)
			continue
		}
		var actualFd descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(fd[0], &actualFd); err != nil {
			t.Errorf("fileDescContainingExtension(%q) return invalid bytes: %v", test.st, err)
			continue
		}
		if !proto.Equal(&actualFd, test.want) {
			t.Errorf("fileDescContainingExtension(%q) returned %q, but wanted %q", test.st, &actualFd, test.want)
		}
	}
}

func (xv1) TestAllExtensionNumbersForTypeName(t *testing.T) {
	for _, test := range []struct {
		st   string
		want []int32
	}{
		{"grpc.testing.ToBeExtended", []int32{13, 17, 19, 23, 29}},
	} {
		r, err := sv1.allExtensionNumbersForTypeName(test.st)
		sort.Sort(intArray(r))
		if err != nil || !reflect.DeepEqual(r, test.want) {
			t.Errorf("allExtensionNumbersForType(%q) = %v, %v, want %v, <nil>", test.st, r, err, test.want)
		}
	}
}

// Do end2end tests.

func (xv1) TestReflectionEnd2end(t *testing.T) {
	// Start server.
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterSearchServiceServer(s, &server{})
	pbv3.RegisterSearchServiceV3Server(s, &serverV3{})

	registerDynamicProto(s, fdDynamic, fdDynamicFile)

	// Register reflection service on s.
	RegisterV1(s)
	go s.Serve(lis)

	// Create client.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}
	defer conn.Close()

	c := rv1pb.NewServerReflectionClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
	defer cancel()
	stream, err := c.ServerReflectionInfo(ctx, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("cannot get ServerReflectionInfo: %v", err)
	}

	testFileByFilenameTransitiveClosureV1(t, stream, true)
	testFileByFilenameTransitiveClosureV1(t, stream, false)
	testFileByFilenameV1(t, stream)
	testFileByFilenameErrorV1(t, stream)
	testFileContainingSymbolV1(t, stream)
	testFileContainingSymbolErrorV1(t, stream)
	testFileContainingExtensionV1(t, stream)
	testFileContainingExtensionErrorV1(t, stream)
	testAllExtensionNumbersOfTypeV1(t, stream)
	testAllExtensionNumbersOfTypeErrorV1(t, stream)
	testListServicesV1(t, stream)

	s.Stop()
}

func testFileByFilenameTransitiveClosureV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient, expectClosure bool) {
	filename := "reflection/grpc_testing/proto2_ext2.proto"
	if err := stream.Send(&rv1pb.ServerReflectionRequest{
		MessageRequest: &rv1pb.ServerReflectionRequest_FileByFilename{
			FileByFilename: filename,
		},
	}); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("failed to recv response: %v", err)
	}
	switch r.MessageResponse.(type) {
	case *rv1pb.ServerReflectionResponse_FileDescriptorResponse:
		if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], fdProto2Ext2Byte) {
			t.Errorf("FileByFilename(%v)\nreceived: %q,\nwant: %q", filename, r.GetFileDescriptorResponse().FileDescriptorProto[0], fdProto2Ext2Byte)
		}
		if expectClosure {
			if len(r.GetFileDescriptorResponse().FileDescriptorProto) != 2 {
				t.Errorf("FileByFilename(%v) returned %v file descriptors, expected 2", filename, len(r.GetFileDescriptorResponse().FileDescriptorProto))
			} else if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[1], fdProto2Byte) {
				t.Errorf("FileByFilename(%v)\nreceived: %q,\nwant: %q", filename, r.GetFileDescriptorResponse().FileDescriptorProto[1], fdProto2Byte)
			}
		} else if len(r.GetFileDescriptorResponse().FileDescriptorProto) != 1 {
			t.Errorf("FileByFilename(%v) returned %v file descriptors, expected 1", filename, len(r.GetFileDescriptorResponse().FileDescriptorProto))
		}
	default:
		t.Errorf("FileByFilename(%v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", filename, r.MessageResponse)
	}
}

func testFileByFilenameV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		filename string
		want     []byte
	}{
		{"reflection/grpc_testing/test.proto", fdTestByte},
		{"reflection/grpc_testing/proto2.proto", fdProto2Byte},
		{"reflection/grpc_testing/proto2_ext.proto", fdProto2ExtByte},
		{"dynamic.proto", fdDynamicByte},
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileByFilename{
				FileByFilename: test.filename,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileByFilename(%v)\nreceived: %q,\nwant: %q", test.filename, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileByFilename(%v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.filename, r.MessageResponse)
		}
	}
}

func testFileByFilenameErrorV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"test.poto",
		"proo2.proto",
		"proto2_et.proto",
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileByFilename{
				FileByFilename: test,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileByFilename(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testFileContainingSymbolV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		symbol string
		want   []byte
	}{
		{"grpc.testing.SearchService", fdTestByte},
		{"grpc.testing.SearchService.Search", fdTestByte},
		{"grpc.testing.SearchService.StreamingSearch", fdTestByte},
		{"grpc.testing.SearchResponse", fdTestByte},
		{"grpc.testing.ToBeExtended", fdProto2Byte},
		// Test support package v3.
		{"grpc.testingv3.SearchServiceV3", fdTestv3Byte},
		{"grpc.testingv3.SearchServiceV3.Search", fdTestv3Byte},
		{"grpc.testingv3.SearchServiceV3.StreamingSearch", fdTestv3Byte},
		{"grpc.testingv3.SearchResponseV3", fdTestv3Byte},
		// search for field, oneof, enum, and enum value symbols, too
		{"grpc.testingv3.SearchResponseV3.Result.snippets", fdTestv3Byte},
		{"grpc.testingv3.SearchResponseV3.Result.Value.val", fdTestv3Byte},
		{"grpc.testingv3.SearchResponseV3.Result.Value.str", fdTestv3Byte},
		{"grpc.testingv3.SearchResponseV3.State", fdTestv3Byte},
		{"grpc.testingv3.SearchResponseV3.FRESH", fdTestv3Byte},
		// Test dynamic symbols
		{"grpc.testing.DynamicService", fdDynamicByte},
		{"grpc.testing.DynamicReq", fdDynamicByte},
		{"grpc.testing.DynamicRes", fdDynamicByte},
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileContainingSymbol{
				FileContainingSymbol: test.symbol,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileContainingSymbol(%v)\nreceived: %q,\nwant: %q", test.symbol, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileContainingSymbol(%v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.symbol, r.MessageResponse)
		}
	}
}

func testFileContainingSymbolErrorV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"grpc.testing.SerchService",
		"grpc.testing.SearchService.SearchE",
		"grpc.tesing.SearchResponse",
		"gpc.testing.ToBeExtended",
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileContainingSymbol{
				FileContainingSymbol: test,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileContainingSymbol(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testFileContainingExtensionV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		extNum   int32
		want     []byte
	}{
		{"grpc.testing.ToBeExtended", 13, fdProto2ExtByte},
		{"grpc.testing.ToBeExtended", 17, fdProto2ExtByte},
		{"grpc.testing.ToBeExtended", 19, fdProto2ExtByte},
		{"grpc.testing.ToBeExtended", 23, fdProto2Ext2Byte},
		{"grpc.testing.ToBeExtended", 29, fdProto2Ext2Byte},
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &rv1pb.ExtensionRequest{
					ContainingType:  test.typeName,
					ExtensionNumber: test.extNum,
				},
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileContainingExtension(%v, %v)\nreceived: %q,\nwant: %q", test.typeName, test.extNum, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileContainingExtension(%v, %v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.typeName, test.extNum, r.MessageResponse)
		}
	}
}

func testFileContainingExtensionErrorV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		extNum   int32
	}{
		{"grpc.testing.ToBExtended", 17},
		{"grpc.testing.ToBeExtended", 15},
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &rv1pb.ExtensionRequest{
					ContainingType:  test.typeName,
					ExtensionNumber: test.extNum,
				},
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileContainingExtension(%v, %v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.typeName, test.extNum, r.MessageResponse)
		}
	}
}

func testAllExtensionNumbersOfTypeV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		want     []int32
	}{
		{"grpc.testing.ToBeExtended", []int32{13, 17, 19, 23, 29}},
		{"grpc.testing.DynamicReq", nil},
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_AllExtensionNumbersOfType{
				AllExtensionNumbersOfType: test.typeName,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_AllExtensionNumbersResponse:
			extNum := r.GetAllExtensionNumbersResponse().ExtensionNumber
			sort.Sort(intArray(extNum))
			if r.GetAllExtensionNumbersResponse().BaseTypeName != test.typeName ||
				!reflect.DeepEqual(extNum, test.want) {
				t.Errorf("AllExtensionNumbersOfType(%v)\nreceived: %v,\nwant: {%q %v}", r.GetAllExtensionNumbersResponse(), test.typeName, test.typeName, test.want)
			}
		default:
			t.Errorf("AllExtensionNumbersOfType(%v) = %v, want type <ServerReflectionResponse_AllExtensionNumbersResponse>", test.typeName, r.MessageResponse)
		}
	}
}

func testAllExtensionNumbersOfTypeErrorV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"grpc.testing.ToBeExtendedE",
	} {
		if err := stream.Send(&rv1pb.ServerReflectionRequest{
			MessageRequest: &rv1pb.ServerReflectionRequest_AllExtensionNumbersOfType{
				AllExtensionNumbersOfType: test,
			},
		}); err != nil {
			t.Fatalf("failed to send request: %v", err)
		}
		r, err := stream.Recv()
		if err != nil {
			// io.EOF is not ok.
			t.Fatalf("failed to recv response: %v", err)
		}

		switch r.MessageResponse.(type) {
		case *rv1pb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("AllExtensionNumbersOfType(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testListServicesV1(t *testing.T, stream rv1pb.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&rv1pb.ServerReflectionRequest{
		MessageRequest: &rv1pb.ServerReflectionRequest_ListServices{},
	}); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("failed to recv response: %v", err)
	}

	switch r.MessageResponse.(type) {
	case *rv1pb.ServerReflectionResponse_ListServicesResponse:
		services := r.GetListServicesResponse().Service
		want := []string{
			"grpc.testingv3.SearchServiceV3",
			"grpc.testing.SearchService",
			"grpc.reflection.v1.ServerReflection",
			"grpc.testing.DynamicService",
		}
		// Compare service names in response with want.
		if len(services) != len(want) {
			t.Errorf("= %v, want service names: %v", services, want)
		}
		m := make(map[string]int)
		for _, e := range services {
			m[e.Name]++
		}
		for _, e := range want {
			if m[e] > 0 {
				m[e]--
				continue
			}
			t.Errorf("ListService\nreceived: %v,\nwant: %q", services, want)
		}
	default:
		t.Errorf("ListServices = %v, want type <ServerReflectionResponse_ListServicesResponse>", r.MessageResponse)
	}
}

// func registerDynamicProto(srv *grpc.Server, fdp *descriptorpb.FileDescriptorProto, fd protoreflect.FileDescriptor) {
// 	type emptyInterface interface{}

// 	for i := 0; i < fd.Services().Len(); i++ {
// 		s := fd.Services().Get(i)

// 		sd := &grpc.ServiceDesc{
// 			ServiceName: string(s.FullName()),
// 			HandlerType: (*emptyInterface)(nil),
// 			Metadata:    fdp.GetName(),
// 		}

// 		for j := 0; j < s.Methods().Len(); j++ {
// 			m := s.Methods().Get(j)
// 			sd.Methods = append(sd.Methods, grpc.MethodDesc{
// 				MethodName: string(m.Name()),
// 			})
// 		}

// 		srv.RegisterService(sd, struct{}{})
// 	}
// }
