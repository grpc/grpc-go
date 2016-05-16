package main

import (
	"reflect"
	"testing"

	pb "google.golang.org/grpc/reflection/grpc_testing"
)

var (
	s = newServerReflectionServer()
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
