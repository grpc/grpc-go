package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestProtocBinary(t *testing.T) {
	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	tmpDir, err := os.MkdirTemp(workingDir, "test")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}

	defer t.Cleanup(func() {
		err := os.RemoveAll(tmpDir)
		if err != nil {
			t.Fatalf("error removing temp dir and all test files: %v", err)
		}
	})

	for _, spec := range []struct {
		protoFile string
	}{
		{
			protoFile: "test_unary.proto",
		},
		{
			protoFile: "test_server_streaming.proto",
		},
		{
			protoFile: "test_client_streaming.proto",
		},
		{
			protoFile: "test_bidirectional_streaming.proto",
		},
	} {
		command := exec.Command("protoc", "--go-grpc_out="+tmpDir, spec.protoFile)
		command.Dir = workingDir

		if err := command.Run(); err != nil {
			t.Fatalf("failed to execute protoc: %v", err)
		}

		pbgoFile := strings.Replace(spec.protoFile, ".proto", "_grpc.pb.go", 1)

		testPluginContent, err := os.ReadFile(tmpDir + "/testdata/proto/" + pbgoFile)
		if err != nil {
			t.Errorf("Error while reading proto file generated from test: %s", err)
		}

		protocPluginContent, err := os.ReadFile("./testdata/proto/" + pbgoFile)
		if err != nil {
			t.Errorf("Error while reading proto file generated from protoc command: %s", err)
		}

		if !reflect.DeepEqual(testPluginContent, protocPluginContent) {
			t.Errorf("Error generated go test stubs are not equal")
		}
	}
}
