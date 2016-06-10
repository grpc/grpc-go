package reflection

import (
	"net"
	"reflect"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"
	dpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	rpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	pb "google.golang.org/grpc/reflection/grpc_testing"
)

var (
	s = &serverReflectionServer{
		// s:                 s,
		typeToNameMap:     make(map[reflect.Type]string),
		nameToTypeMap:     make(map[string]reflect.Type),
		typeToFileDescMap: make(map[reflect.Type]*dpb.FileDescriptorProto),
		filenameToDescMap: make(map[string]*dpb.FileDescriptorProto),
	}
)

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

func TestNameForType(t *testing.T) {
	for _, test := range []struct {
		st   reflect.Type
		want string
	}{
		{reflect.TypeOf(pb.SearchResponse{}), "grpc.testing.SearchResponse"},
		{reflect.TypeOf(pb.SearchResponse_Result{}), "grpc.testing.SearchResponse_Result"},
	} {
		r, err := s.nameForType(test.st)
		t.Logf("nameForType(%q) = %q, %v", test.st, r, err)
		if err != nil || r != test.want {
			t.Fatalf("nameForType(%q) = %q, %v, want %q, <nil>", test.st, r, err, test.want)
		}
	}
}

func TestNameForPointer(t *testing.T) {
	for _, test := range []struct {
		pt   interface{}
		want string
	}{
		{&pb.SearchResponse{}, "grpc.testing.SearchResponse"},
		{&pb.SearchResponse_Result{}, "grpc.testing.SearchResponse_Result"},
	} {
		r, err := s.nameForPointer(test.pt)
		t.Logf("nameForPointer(%T) = %q, %v", test.pt, r, err)
		if err != nil || r != test.want {
			t.Fatalf("nameForPointer(%T) = %q, %v, want %q, <nil>", test.pt, r, err, test.want)
		}
	}
}

func TestFilenameForType(t *testing.T) {
	for _, test := range []struct {
		st   reflect.Type
		want string
	}{
		{reflect.TypeOf(pb.SearchResponse{}), "test.proto"},
		{reflect.TypeOf(pb.SearchResponse_Result{}), "test.proto"},
	} {
		r, err := s.filenameForType(test.st)
		t.Logf("filenameForType(%q) = %q, %v", test.st, r, err)
		if err != nil || r != test.want {
			t.Fatalf("filenameForType(%q) = %q, %v, want %q, <nil>", test.st, r, err, test.want)
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
	if err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_FileByFilename{
			FileByFilename: "blah",
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
	// TODO check response
}

func testFileContainingSymbol(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: "grpc.testing.SearchResponse",
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
	// TODO check response
}

func testFileContainingExtension(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_FileContainingExtension{
			FileContainingExtension: &rpb.ExtensionRequest{
				ContainingType:  "grpc.testing.ToBeExtened",
				ExtensionNumber: 17,
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
	// TODO check response
}

func testAllExtensionNumbersOfType(t *testing.T, stream rpb.ServerReflection_ServerReflectionInfoClient) {
	if err := stream.Send(&rpb.ServerReflectionRequest{
		MessageRequest: &rpb.ServerReflectionRequest_AllExtensionNumbersOfType{
			AllExtensionNumbersOfType: "grpc.testing.ToBeExtened",
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
	// TODO check response
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
}
