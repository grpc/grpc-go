package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io/ioutil"
	"reflect"
	"sync"

	"github.com/golang/protobuf/proto"
	dpb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type serverReflectionServer struct {
	mu                sync.Mutex
	typeToNameMap     map[reflect.Type]string
	nameToTypeMap     map[string]reflect.Type
	typeToFileDescMap map[reflect.Type]*dpb.FileDescriptorProto
	filenameToDescMap map[string]*dpb.FileDescriptorProto
}

func newServerReflectionServer() *serverReflectionServer {
	return &serverReflectionServer{
		typeToNameMap:     make(map[reflect.Type]string),
		nameToTypeMap:     make(map[string]reflect.Type),
		typeToFileDescMap: make(map[reflect.Type]*dpb.FileDescriptorProto),
		filenameToDescMap: make(map[string]*dpb.FileDescriptorProto),
	}
}

type protoMessage interface {
	Descriptor() ([]byte, []int)
}

// TODO return an error rather than a bool
func (s *serverReflectionServer) fileDescForType(st reflect.Type) (*dpb.FileDescriptorProto, []int, error) {
	m, ok := reflect.Zero(reflect.PtrTo(st)).Interface().(protoMessage)
	if !ok {
		// TODO better print content.
		return nil, nil, fmt.Errorf("failed to create message from type: %v", st)
	}
	enc, idxs := m.Descriptor()

	// TODO Check cache first.
	// if fn, ok := s.typeToFilenameMap[st]; ok {
	// 	if fd, ok := s.filenameToDescMap[fn]; ok {
	// 		return fd, idxs, ok
	// 	}
	// }

	fd, err := s.decodeFileDesc(enc)
	if err != nil {
		return nil, nil, err
	}
	// TODO Cache missed, add to cache.
	// s.typeToFilenameMap[st] = fd.GetName()
	return fd, idxs, nil
}

func (s *serverReflectionServer) decodeFileDesc(enc []byte) (*dpb.FileDescriptorProto, error) {
	raw := decompress(enc)
	if raw == nil {
		return nil, fmt.Errorf("failed to decompress enc")
	}

	fd := new(dpb.FileDescriptorProto)
	if err := proto.Unmarshal(raw, fd); err != nil {
		return nil, fmt.Errorf("bad descriptor: %v", err)
	}
	// TODO If decodeFileDesc is called, it's the first time this file is seen.
	// Add it to cache.
	// s.filenameToDescMap[fd.GetName()] = fd
	return fd, nil
}

func decompress(b []byte) []byte {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		fmt.Printf("bad gzipped descriptor: %v\n", err)
		return nil
	}
	out, err := ioutil.ReadAll(r)
	if err != nil {
		fmt.Printf("bad gzipped descriptor: %v\n", err)
		return nil
	}
	return out
}
