package main

import (
	"reflect"
	"sort"
	"testing"

	pb "google.golang.org/grpc/reflection/grpc_testing"
)

var (
	s = newServerReflectionServer()
)

// TODO TestFileDescForType(t *testing.T)
