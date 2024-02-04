/*
 *
 * Copyright 2024 gRPC authors.
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

	protocPath, err := exec.LookPath("protoc")
	if err != nil {
		t.Fatalf("protoc not found in PATH: %v", err)
	}

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
		protoFilePath := "./proto/" + spec.protoFile
		if _, err := os.Stat(protoFilePath); os.IsNotExist(err) {
			t.Fatalf("proto file %s does not exist", protoFilePath)
		}

		command := exec.Command(protocPath, "--go-grpc_out="+tmpDir, protoFilePath)
		command.Dir = workingDir

		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to execute command protoc: %v\n%s", err, output)
		}

		pbgoFile := strings.Replace(spec.protoFile, ".proto", "_grpc.pb.go", 1)

		testPluginContent, err := os.ReadFile(tmpDir + "/testdata/proto/" + pbgoFile)
		if err != nil {
			t.Errorf("error while reading proto file generated from test: %s", err)
		}

		protocPluginContent, err := os.ReadFile("./testdata/proto/" + pbgoFile)
		if err != nil {
			t.Errorf("error while reading proto file generated from protoc command: %s", err)
		}

		if !reflect.DeepEqual(testPluginContent, protocPluginContent) {
			t.Errorf("error generated go test stubs are not equal")
		}
	}
}
