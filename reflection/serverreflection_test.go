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
	"fmt"
	"net"
	"reflect"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/internal/grpctest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	v1reflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1"
	v1reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	v1alphareflectiongrpc "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	v1alphareflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	pb "google.golang.org/grpc/reflection/grpc_testing"
	pbv3 "google.golang.org/grpc/reflection/grpc_testing_not_regenerate"
)

var (
	s = NewServerV1(ServerOptions{}).(*serverReflectionServer)
	// fileDescriptor of each test proto file.
	fdProto2Ext  *descriptorpb.FileDescriptorProto
	fdProto2Ext2 *descriptorpb.FileDescriptorProto
	fdDynamic    *descriptorpb.FileDescriptorProto
	// reflection descriptors.
	fdDynamicFile protoreflect.FileDescriptor
	// fileDescriptor marshalled.
	fdTestByte       []byte
	fdTestv3Byte     []byte
	fdProto2Byte     []byte
	fdProto2ExtByte  []byte
	fdProto2Ext2Byte []byte
	fdDynamicByte    []byte
)

const defaultTestTimeout = 10 * time.Second

type x struct {
	grpctest.Tester
}

func Test(t *testing.T) {
	grpctest.RunSubTests(t, x{})
}

func loadFileDesc(filename string) (*descriptorpb.FileDescriptorProto, []byte) {
	fd, err := protoregistry.GlobalFiles.FindFileByPath(filename)
	if err != nil {
		panic(err)
	}
	fdProto := protodesc.ToFileDescriptorProto(fd)
	b, err := proto.Marshal(fdProto)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal fd: %v", err))
	}
	return fdProto, b
}

func loadFileDescDynamic(b []byte) (*descriptorpb.FileDescriptorProto, protoreflect.FileDescriptor, []byte) {
	m := new(descriptorpb.FileDescriptorProto)
	if err := proto.Unmarshal(b, m); err != nil {
		panic("failed to unmarshal dynamic proto raw descriptor")
	}

	fd, err := protodesc.NewFile(m, nil)
	if err != nil {
		panic(err)
	}

	err = protoregistry.GlobalFiles.RegisterFile(fd)
	if err != nil {
		panic(err)
	}

	for i := 0; i < fd.Messages().Len(); i++ {
		m := fd.Messages().Get(i)
		if err := protoregistry.GlobalTypes.RegisterMessage(dynamicpb.NewMessageType(m)); err != nil {
			panic(err)
		}
	}

	return m, fd, b
}

func init() {
	_, fdTestByte = loadFileDesc("reflection/grpc_testing/test.proto")
	_, fdTestv3Byte = loadFileDesc("testv3.proto")
	_, fdProto2Byte = loadFileDesc("reflection/grpc_testing/proto2.proto")
	fdProto2Ext, fdProto2ExtByte = loadFileDesc("reflection/grpc_testing/proto2_ext.proto")
	fdProto2Ext2, fdProto2Ext2Byte = loadFileDesc("reflection/grpc_testing/proto2_ext2.proto")
	fdDynamic, fdDynamicFile, fdDynamicByte = loadFileDescDynamic(pbv3.FileDynamicProtoRawDesc)
}

func (x) TestFileDescContainingExtension(t *testing.T) {
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
		fd, err := s.fileDescEncodingContainingExtension(test.st, test.extNum, map[string]bool{})
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

// intArray is used to sort []int32
type intArray []int32

func (s intArray) Len() int           { return len(s) }
func (s intArray) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s intArray) Less(i, j int) bool { return s[i] < s[j] }

func (x) TestAllExtensionNumbersForTypeName(t *testing.T) {
	for _, test := range []struct {
		st   string
		want []int32
	}{
		{"grpc.testing.ToBeExtended", []int32{13, 17, 19, 23, 29}},
	} {
		r, err := s.allExtensionNumbersForTypeName(test.st)
		sort.Sort(intArray(r))
		if err != nil || !reflect.DeepEqual(r, test.want) {
			t.Errorf("allExtensionNumbersForType(%q) = %v, %v, want %v, <nil>", test.st, r, err, test.want)
		}
	}
}

func (x) TestFileDescWithDependencies(t *testing.T) {
	depFile, err := protodesc.NewFile(
		&descriptorpb.FileDescriptorProto{
			Name: proto.String("dep.proto"),
		}, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	deps := &protoregistry.Files{}
	if err := deps.RegisterFile(depFile); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	rootFileProto := &descriptorpb.FileDescriptorProto{
		Name: proto.String("root.proto"),
		Dependency: []string{
			"google/protobuf/descriptor.proto",
			"reflection/grpc_testing/proto2_ext2.proto",
			"dep.proto",
		},
	}

	// dep.proto is in deps; the other imports come from protoregistry.GlobalFiles
	resolver := &combinedResolver{first: protoregistry.GlobalFiles, second: deps}
	rootFile, err := protodesc.NewFile(rootFileProto, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Create a file hierarchy that contains a placeholder for dep.proto
	placeholderDep := placeholderFile{depFile}
	placeholderDeps := &protoregistry.Files{}
	if err := placeholderDeps.RegisterFile(placeholderDep); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	resolver = &combinedResolver{first: protoregistry.GlobalFiles, second: placeholderDeps}

	rootFileHasPlaceholderDep, err := protodesc.NewFile(rootFileProto, resolver)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	rootFileIsPlaceholder := placeholderFile{rootFile}

	// Full transitive dependency graph of root.proto includes five files:
	// - root.proto
	//   - google/protobuf/descriptor.proto
	//   - reflection/grpc_testing/proto2_ext2.proto
	//     - reflection/grpc_testing/proto2.proto
	//   - dep.proto

	for _, test := range []struct {
		name   string
		sent   []string
		root   protoreflect.FileDescriptor
		expect []string
	}{
		{
			name: "send_all",
			root: rootFile,
			// expect full transitive closure
			expect: []string{
				"root.proto",
				"google/protobuf/descriptor.proto",
				"reflection/grpc_testing/proto2_ext2.proto",
				"reflection/grpc_testing/proto2.proto",
				"dep.proto",
			},
		},
		{
			name: "already_sent",
			sent: []string{
				"root.proto",
				"google/protobuf/descriptor.proto",
				"reflection/grpc_testing/proto2_ext2.proto",
				"reflection/grpc_testing/proto2.proto",
				"dep.proto",
			},
			root: rootFile,
			// expect only the root to be re-sent
			expect: []string{"root.proto"},
		},
		{
			name: "some_already_sent",
			sent: []string{
				"reflection/grpc_testing/proto2_ext2.proto",
				"reflection/grpc_testing/proto2.proto",
			},
			root: rootFile,
			expect: []string{
				"root.proto",
				"google/protobuf/descriptor.proto",
				"dep.proto",
			},
		},
		{
			name: "root_is_placeholder",
			root: rootFileIsPlaceholder,
			// expect error, no files
		},
		{
			name: "placeholder_skipped",
			root: rootFileHasPlaceholderDep,
			// dep.proto is a placeholder so is skipped
			expect: []string{
				"root.proto",
				"google/protobuf/descriptor.proto",
				"reflection/grpc_testing/proto2_ext2.proto",
				"reflection/grpc_testing/proto2.proto",
			},
		},
		{
			name: "placeholder_skipped_and_some_sent",
			sent: []string{
				"reflection/grpc_testing/proto2_ext2.proto",
				"reflection/grpc_testing/proto2.proto",
			},
			root: rootFileHasPlaceholderDep,
			expect: []string{
				"root.proto",
				"google/protobuf/descriptor.proto",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := NewServerV1(ServerOptions{}).(*serverReflectionServer)

			sent := map[string]bool{}
			for _, path := range test.sent {
				sent[path] = true
			}

			descriptors, err := s.fileDescWithDependencies(test.root, sent)
			if len(test.expect) == 0 {
				// if we're not expecting any files then we're expecting an error
				if err == nil {
					t.Fatalf("expecting an error; instead got %d files", len(descriptors))
				}
				return
			}

			checkDescriptorResults(t, descriptors, test.expect)
		})
	}
}

func checkDescriptorResults(t *testing.T, descriptors [][]byte, expect []string) {
	t.Helper()
	if len(descriptors) != len(expect) {
		t.Errorf("expected result to contain %d descriptor(s); instead got %d", len(expect), len(descriptors))
	}
	names := map[string]struct{}{}
	for i, desc := range descriptors {
		var descProto descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(desc, &descProto); err != nil {
			t.Fatalf("could not unmarshal descriptor result #%d", i+1)
		}
		names[descProto.GetName()] = struct{}{}
	}
	actual := make([]string, 0, len(names))
	for name := range names {
		actual = append(actual, name)
	}
	sort.Strings(actual)
	sort.Strings(expect)
	if !reflect.DeepEqual(actual, expect) {
		t.Fatalf("expected file descriptors for %v; instead got %v", expect, actual)
	}
}

type placeholderFile struct {
	protoreflect.FileDescriptor
}

func (placeholderFile) IsPlaceholder() bool {
	return true
}

type combinedResolver struct {
	first, second protodesc.Resolver
}

func (r *combinedResolver) FindFileByPath(path string) (protoreflect.FileDescriptor, error) {
	file, err := r.first.FindFileByPath(path)
	if err == nil {
		return file, nil
	}
	return r.second.FindFileByPath(path)
}

func (r *combinedResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	desc, err := r.first.FindDescriptorByName(name)
	if err == nil {
		return desc, nil
	}
	return r.second.FindDescriptorByName(name)
}

// Do end2end tests.

type server struct {
	pb.UnimplementedSearchServiceServer
}

func (s *server) Search(ctx context.Context, in *pb.SearchRequest) (*pb.SearchResponse, error) {
	return &pb.SearchResponse{}, nil
}

func (s *server) StreamingSearch(stream pb.SearchService_StreamingSearchServer) error {
	return nil
}

type serverV3 struct{}

func (s *serverV3) Search(ctx context.Context, in *pbv3.SearchRequestV3) (*pbv3.SearchResponseV3, error) {
	return &pbv3.SearchResponseV3{}, nil
}

func (s *serverV3) StreamingSearch(stream pbv3.SearchServiceV3_StreamingSearchServer) error {
	return nil
}

func (x) TestReflectionEnd2end(t *testing.T) {
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
	Register(s)
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	// Create client.
	conn, err := grpc.Dial(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}
	defer conn.Close()

	clientV1 := v1reflectiongrpc.NewServerReflectionClient(conn)
	clientV1Alpha := v1alphareflectiongrpc.NewServerReflectionClient(conn)
	testCases := []struct {
		name   string
		client v1reflectiongrpc.ServerReflectionClient
	}{
		{
			name:   "v1",
			client: clientV1,
		},
		{
			name:   "v1alpha",
			client: v1AlphaClientAdapter{stub: clientV1Alpha},
		},
	}
	for _, testCase := range testCases {
		c := testCase.client
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTestTimeout)
			defer cancel()
			stream, err := c.ServerReflectionInfo(ctx, grpc.WaitForReady(true))
			if err != nil {
				t.Fatalf("cannot get ServerReflectionInfo: %v", err)
			}

			testFileByFilenameTransitiveClosure(t, stream, true)
			testFileByFilenameTransitiveClosure(t, stream, false)
			testFileByFilename(t, stream)
			testFileByFilenameError(t, stream)
			testFileContainingSymbol(t, stream)
			testFileContainingSymbolError(t, stream)
			testFileContainingExtension(t, stream)
			testFileContainingExtensionError(t, stream)
			testAllExtensionNumbersOfType(t, stream)
			testAllExtensionNumbersOfTypeError(t, stream)
			testListServices(t, stream)
		})
	}
}

func testFileByFilenameTransitiveClosure(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient, expectClosure bool) {
	filename := "reflection/grpc_testing/proto2_ext2.proto"
	if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
		MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileByFilename{
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
	case *v1reflectionpb.ServerReflectionResponse_FileDescriptorResponse:
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

func testFileByFilename(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		filename string
		want     []byte
	}{
		{"reflection/grpc_testing/test.proto", fdTestByte},
		{"reflection/grpc_testing/proto2.proto", fdProto2Byte},
		{"reflection/grpc_testing/proto2_ext.proto", fdProto2ExtByte},
		{"dynamic.proto", fdDynamicByte},
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileByFilename{
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
		case *v1reflectionpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileByFilename(%v)\nreceived: %q,\nwant: %q", test.filename, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileByFilename(%v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.filename, r.MessageResponse)
		}
	}
}

func testFileByFilenameError(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"test.poto",
		"proo2.proto",
		"proto2_et.proto",
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileByFilename{
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
		case *v1reflectionpb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileByFilename(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testFileContainingSymbol(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
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
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileContainingSymbol{
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
		case *v1reflectionpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileContainingSymbol(%v)\nreceived: %q,\nwant: %q", test.symbol, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileContainingSymbol(%v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.symbol, r.MessageResponse)
		}
	}
}

func testFileContainingSymbolError(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"grpc.testing.SerchService",
		"grpc.testing.SearchService.SearchE",
		"grpc.tesing.SearchResponse",
		"gpc.testing.ToBeExtended",
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileContainingSymbol{
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
		case *v1reflectionpb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileContainingSymbol(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testFileContainingExtension(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
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
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &v1reflectionpb.ExtensionRequest{
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
		case *v1reflectionpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Errorf("FileContainingExtension(%v, %v)\nreceived: %q,\nwant: %q", test.typeName, test.extNum, r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Errorf("FileContainingExtension(%v, %v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.typeName, test.extNum, r.MessageResponse)
		}
	}
}

func testFileContainingExtensionError(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		extNum   int32
	}{
		{"grpc.testing.ToBExtended", 17},
		{"grpc.testing.ToBeExtended", 15},
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &v1reflectionpb.ExtensionRequest{
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
		case *v1reflectionpb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("FileContainingExtension(%v, %v) = %v, want type <ServerReflectionResponse_FileDescriptorResponse>", test.typeName, test.extNum, r.MessageResponse)
		}
	}
}

func testAllExtensionNumbersOfType(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		want     []int32
	}{
		{"grpc.testing.ToBeExtended", []int32{13, 17, 19, 23, 29}},
		{"grpc.testing.DynamicReq", nil},
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_AllExtensionNumbersOfType{
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
		case *v1reflectionpb.ServerReflectionResponse_AllExtensionNumbersResponse:
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

func testAllExtensionNumbersOfTypeError(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []string{
		"grpc.testing.ToBeExtendedE",
	} {
		if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
			MessageRequest: &v1reflectionpb.ServerReflectionRequest_AllExtensionNumbersOfType{
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
		case *v1reflectionpb.ServerReflectionResponse_ErrorResponse:
		default:
			t.Errorf("AllExtensionNumbersOfType(%v) = %v, want type <ServerReflectionResponse_ErrorResponse>", test, r.MessageResponse)
		}
	}
}

func testListServices(t *testing.T, stream v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&v1reflectionpb.ServerReflectionRequest{
		MessageRequest: &v1reflectionpb.ServerReflectionRequest_ListServices{},
	}); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("failed to recv response: %v", err)
	}

	switch r.MessageResponse.(type) {
	case *v1reflectionpb.ServerReflectionResponse_ListServicesResponse:
		services := r.GetListServicesResponse().Service
		want := []string{
			"grpc.testingv3.SearchServiceV3",
			"grpc.testing.SearchService",
			"grpc.reflection.v1.ServerReflection",
			"grpc.reflection.v1alpha.ServerReflection",
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

func registerDynamicProto(srv *grpc.Server, fdp *descriptorpb.FileDescriptorProto, fd protoreflect.FileDescriptor) {
	type emptyInterface any

	for i := 0; i < fd.Services().Len(); i++ {
		s := fd.Services().Get(i)

		sd := &grpc.ServiceDesc{
			ServiceName: string(s.FullName()),
			HandlerType: (*emptyInterface)(nil),
			Metadata:    fdp.GetName(),
		}

		for j := 0; j < s.Methods().Len(); j++ {
			m := s.Methods().Get(j)
			sd.Methods = append(sd.Methods, grpc.MethodDesc{
				MethodName: string(m.Name()),
			})
		}

		srv.RegisterService(sd, struct{}{})
	}
}

type v1AlphaClientAdapter struct {
	stub v1alphareflectiongrpc.ServerReflectionClient
}

func (v v1AlphaClientAdapter) ServerReflectionInfo(ctx context.Context, opts ...grpc.CallOption) (v1reflectiongrpc.ServerReflection_ServerReflectionInfoClient, error) {
	stream, err := v.stub.ServerReflectionInfo(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return v1AlphaClientStreamAdapter{stream}, nil
}

type v1AlphaClientStreamAdapter struct {
	v1alphareflectiongrpc.ServerReflection_ServerReflectionInfoClient
}

func (s v1AlphaClientStreamAdapter) Send(request *v1reflectionpb.ServerReflectionRequest) error {
	return s.ServerReflection_ServerReflectionInfoClient.Send(v1ToV1AlphaRequest(request))
}

func (s v1AlphaClientStreamAdapter) Recv() (*v1reflectionpb.ServerReflectionResponse, error) {
	resp, err := s.ServerReflection_ServerReflectionInfoClient.Recv()
	if err != nil {
		return nil, err
	}
	return v1AlphaToV1Response(resp), nil
}

func v1AlphaToV1Response(v1alpha *v1alphareflectionpb.ServerReflectionResponse) *v1reflectionpb.ServerReflectionResponse {
	var v1 v1reflectionpb.ServerReflectionResponse
	v1.ValidHost = v1alpha.ValidHost
	if v1alpha.OriginalRequest != nil {
		v1.OriginalRequest = v1AlphaToV1Request(v1alpha.OriginalRequest)
	}
	switch mr := v1alpha.MessageResponse.(type) {
	case *v1alphareflectionpb.ServerReflectionResponse_FileDescriptorResponse:
		if mr != nil {
			v1.MessageResponse = &v1reflectionpb.ServerReflectionResponse_FileDescriptorResponse{
				FileDescriptorResponse: &v1reflectionpb.FileDescriptorResponse{
					FileDescriptorProto: mr.FileDescriptorResponse.GetFileDescriptorProto(),
				},
			}
		}
	case *v1alphareflectionpb.ServerReflectionResponse_AllExtensionNumbersResponse:
		if mr != nil {
			v1.MessageResponse = &v1reflectionpb.ServerReflectionResponse_AllExtensionNumbersResponse{
				AllExtensionNumbersResponse: &v1reflectionpb.ExtensionNumberResponse{
					BaseTypeName:    mr.AllExtensionNumbersResponse.GetBaseTypeName(),
					ExtensionNumber: mr.AllExtensionNumbersResponse.GetExtensionNumber(),
				},
			}
		}
	case *v1alphareflectionpb.ServerReflectionResponse_ListServicesResponse:
		if mr != nil {
			svcs := make([]*v1reflectionpb.ServiceResponse, len(mr.ListServicesResponse.GetService()))
			for i, svc := range mr.ListServicesResponse.GetService() {
				svcs[i] = &v1reflectionpb.ServiceResponse{
					Name: svc.GetName(),
				}
			}
			v1.MessageResponse = &v1reflectionpb.ServerReflectionResponse_ListServicesResponse{
				ListServicesResponse: &v1reflectionpb.ListServiceResponse{
					Service: svcs,
				},
			}
		}
	case *v1alphareflectionpb.ServerReflectionResponse_ErrorResponse:
		if mr != nil {
			v1.MessageResponse = &v1reflectionpb.ServerReflectionResponse_ErrorResponse{
				ErrorResponse: &v1reflectionpb.ErrorResponse{
					ErrorCode:    mr.ErrorResponse.GetErrorCode(),
					ErrorMessage: mr.ErrorResponse.GetErrorMessage(),
				},
			}
		}
	default:
		// no value set
	}
	return &v1
}
