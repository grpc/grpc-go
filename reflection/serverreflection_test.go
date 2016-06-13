package reflection

import (
	"fmt"
	"net"
	"reflect"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"
	// dpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	pb "google.golang.org/grpc/reflection/grpc_testing"
)

var (
	s = &serverReflectionServer{}
	// fileDescriptorX of each test proto file.
	fdTest      []byte
	fdProto2    []byte
	fdProto2Ext []byte
)

func loadFileDesc(filename string) []byte {
	enc := proto.FileDescriptor(filename)
	if enc == nil {
		panic(fmt.Sprintf("failed to find fd for file: %v", filename))
	}
	fd, err := s.decodeFileDesc(enc)
	if err != nil {
		panic(fmt.Sprintf("failed to decode enc: %v", err))
	}
	b, err := proto.Marshal(fd)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal fd: %v", err))
	}
	return b
}

func init() {
	fdTest = loadFileDesc("test.proto")
	fdProto2 = loadFileDesc("proto2.proto")
	fdProto2Ext = loadFileDesc("proto2_ext.proto")
}

// TODO TestFileDescForType(t *testing.T)

func TestTypeForName(t *testing.T) {
	for _, test := range []struct {
		name string
		want reflect.Type
	}{
		{"grpc.testing.SearchResponse", reflect.TypeOf(pb.SearchResponse{})},
	} {
		r, err := s.typeForName(test.name)
		// TODO remove all Logf
		t.Logf("typeForName(%q) = %q, %v", test.name, r, err)
		if err != nil || r != test.want {
			t.Fatalf("typeForName(%q) = %q, %v, want %q, <nil>", test.name, r, err, test.want)
		}
	}
}

func TestTypeForNameNotFound(t *testing.T) {
	for _, test := range []string{
		"grpc.testing.not_exiting",
	} {
		_, err := s.typeForName(test)
		if err == nil {
			t.Fatalf("typeForName(%q) = _, %v, want _, <non-nil>", test, err)
		}
	}
}

// TODO TestFileDescContainingExtension

// intArray is used to sort []int32
type intArray []int32

func (s intArray) Len() int           { return len(s) }
func (s intArray) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s intArray) Less(i, j int) bool { return s[i] < s[j] }

func TestAllExtensionNumbersForType(t *testing.T) {
	for _, test := range []struct {
		st   reflect.Type
		want []int32
	}{
		{reflect.TypeOf(pb.ToBeExtened{}), []int32{13, 17}},
	} {
		r, err := s.allExtensionNumbersForType(test.st)
		sort.Sort(intArray(r))
		t.Logf("allExtensionNumbersForType(%q) = %v, %v", test.st, r, err)
		if err != nil || !reflect.DeepEqual(r, test.want) {
			t.Fatalf("allExtensionNumbersForType(%q) = %v, %v, want %v, <nil>", test.st, r, err, test.want)
		}
	}
}

// TODO a better test
func TestFileDescWireFormatByFilename(t *testing.T) {
	st := reflect.TypeOf(pb.SearchResponse_Result{})
	fd, _, err := s.fileDescForType(st)
	if err != nil {
		t.Fatalf("failed to do fileDescForType for %q", st)
	}
	wanted, err := proto.Marshal(fd)
	if err != nil {
		t.Fatalf("failed to do Marshal for %q", fd)
	}
	b, err := s.fileDescWireFormatByFilename(fd.GetName())
	t.Logf("fileDescWireFormatByFilename(%q) = %v, %v", fd.GetName(), b, err)
	if err != nil || !reflect.DeepEqual(b, wanted) {
		t.Fatalf("fileDescWireFormatByFilename(%q) = %v, %v, want %v, <nil>", fd.GetName(), b, err, wanted)
	}
}

// Do end2end tests.

var (
	port = ":35764"
)

type server struct{}

func (s *server) Search(ctx context.Context, in *pb.SearchRequest) (*pb.SearchResponse, error) {
	return &pb.SearchResponse{}, nil
}

func (s *server) StreamingSearch(stream pb.SearchService_StreamingSearchServer) error {
	return nil
}

func TestEnd2end(t *testing.T) {
	// Start server.
	lis, err := net.Listen("tcp", port)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	pb.RegisterSearchServiceServer(s, &server{})
	// Install reflection service on s.
	InstallOnServer(s)
	go s.Serve(lis)

	// Create client.
	conn, err := grpc.Dial("localhost"+port, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("cannot connect to server: %v", err)
	}
	defer conn.Close()

	c := rpb.NewServerReflectionClient(conn)
	stream, err := c.ServerReflectionInfo(context.Background())

	testFileByFilename(t, stream)
	testFileContainingSymbol(t, stream)
	testFileContainingExtension(t, stream)
	testAllExtensionNumbersOfType(t, stream)
	testListServices(t, stream)

	s.Stop()
}

func testFileByFilename(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		filename string
		want     []byte
	}{
		{"test.proto", fdTest},
		{"proto2.proto", fdProto2},
		{"proto2_ext.proto", fdProto2Ext},
	} {
		if err := stream.Send(&rpb.ServerReflectionRequest{
			MessageRequest: &rpb.ServerReflectionRequest_FileByFilename{
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

		t.Logf("response: %v", r)
		switch r.MessageResponse.(type) {
		case *rpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Fatalf("= %v, want %v", r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Fatalf("= %v, want type <ServerReflectionResponse_FileDescriptorResponse>", r.MessageResponse)
		}
	}
}

func testFileContainingSymbol(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		symbol string
		want   []byte
	}{
		{"grpc.testing.SearchResponse", fdTest},
		{"grpc.testing.ToBeExtened", fdProto2},
	} {
		if err := stream.Send(&rpb.ServerReflectionRequest{
			MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{
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

		t.Logf("response: %v", r)
		switch r.MessageResponse.(type) {
		case *rpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Fatalf("= %v, want %v", r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Fatalf("= %v, want type <ServerReflectionResponse_FileDescriptorResponse>", r.MessageResponse)
		}
	}
}

func testFileContainingExtension(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		extNum   int32
		want     []byte
	}{
		{"grpc.testing.ToBeExtened", 17, fdProto2Ext},
	} {
		if err := stream.Send(&rpb.ServerReflectionRequest{
			MessageRequest: &rpb.ServerReflectionRequest_FileContainingExtension{
				FileContainingExtension: &rpb.ExtensionRequest{
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

		t.Logf("response: %v", r)
		switch r.MessageResponse.(type) {
		case *rpb.ServerReflectionResponse_FileDescriptorResponse:
			if !reflect.DeepEqual(r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want) {
				t.Fatalf("= %v, want %v", r.GetFileDescriptorResponse().FileDescriptorProto[0], test.want)
			}
		default:
			t.Fatalf("= %v, want type <ServerReflectionResponse_FileDescriptorResponse>", r.MessageResponse)
		}
	}
}

func testAllExtensionNumbersOfType(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	for _, test := range []struct {
		typeName string
		want     []int32
	}{
		{"grpc.testing.ToBeExtened", []int32{13, 17}},
	} {
		if err := stream.Send(&rpb.ServerReflectionRequest{
			MessageRequest: &rpb.ServerReflectionRequest_AllExtensionNumbersOfType{
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

		t.Logf("response: %v", r)
		switch r.MessageResponse.(type) {
		case *rpb.ServerReflectionResponse_AllExtensionNumbersResponse:
			extNum := r.GetAllExtensionNumbersResponse().ExtensionNumber
			sort.Sort(intArray(extNum))
			if r.GetAllExtensionNumbersResponse().BaseTypeName != test.typeName ||
				!reflect.DeepEqual(extNum, test.want) {
				t.Fatalf("= %v, want {%q %v}", r.GetAllExtensionNumbersResponse(), test.typeName, test.want)
			}
		default:
			t.Fatalf("= %v, want type <ServerReflectionResponse_AllExtensionNumbersResponse>", r.MessageResponse)
		}
	}
}

func testListServices(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_ListServices{},
	}); err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	r, err := stream.Recv()
	if err != nil {
		// io.EOF is not ok.
		t.Fatalf("failed to recv response: %v", err)
	}

	t.Logf("response: %v", r)
	// TODO check response
	switch r.MessageResponse.(type) {
	case *rpb.ServerReflectionResponse_ListServicesResponse:
		t.Fatalf("should be error: not implemented")
	default:
		// t.Fatalf("= %v, want type <ServerReflectionResponse_ListServicesResponse>", r.MessageResponse)
	}
}
