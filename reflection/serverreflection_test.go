package reflection

import (
	"reflect"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"
	dpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
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
