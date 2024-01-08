package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func initPlugin() (*protogen.Plugin, error) {
	req := &pluginpb.CodeGeneratorRequest{
		ProtoFile:      initProtoFile(),
		FileToGenerate: []string{"test_unary.proto"},
		CompilerVersion: &pluginpb.Version{
			Major: proto.Int32(4),
			Minor: proto.Int32(24),
			Patch: proto.Int32(3),
		},
	}

	gen, err := protogen.Options{}.New(req)

	if err != nil {
		return nil, err
	}

	return gen, nil
}

func initProtoFile() []*descriptorpb.FileDescriptorProto {
	return []*descriptorpb.FileDescriptorProto{
		initUnaryRPCProto(),
	}
}

func initUnaryRPCProto() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test_unary.proto"),
		Package: proto.String("main"),
		Options: &descriptorpb.FileOptions{
			GoPackage: proto.String("main/proto"),
		},
		MessageType: initUnaryRPCMessageType(),
		Service:     initUnaryRPCService(),
	}
}

func initUnaryRPCService() []*descriptorpb.ServiceDescriptorProto {
	return []*descriptorpb.ServiceDescriptorProto{
		{
			Name:   proto.String("BlogPostService"),
			Method: initUnaryRPCMethod(),
		},
	}
}

func initUnaryRPCMethod() []*descriptorpb.MethodDescriptorProto {
	return []*descriptorpb.MethodDescriptorProto{
		{
			Name:       proto.String("addBlogPost"),
			InputType:  proto.String("EventRequest"),
			OutputType: proto.String("EventResponse"),
		},
	}
}

func initUnaryRPCMessageType() []*descriptorpb.DescriptorProto {
	return []*descriptorpb.DescriptorProto{
		{
			Name: proto.String("EventRequest"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("content"),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
					Number: proto.Int32(1),
				},
			},
		},
		{
			Name: proto.String("EventResponse"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   proto.String("content"),
					Type:   descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
					Number: proto.Int32(1),
				},
			},
		},
	}
}

func TestGenerateFile(t *testing.T) {
	testGeneratedGoProtoPath := "test_unary_grpc.pb.go"

	requireUnimplemented = proto.Bool(true)

	gen, err := initPlugin()

	if err != nil {
		t.Errorf("Error while creating Plugin object: %s", err)
	}

	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}

		file := generateFile(gen, f)
		fileContent, _ := file.Content()
		filePathUnary := strings.Replace(*f.Proto.Name, ".proto", "_grpc.pb.go", 1)

		err := os.WriteFile(filePathUnary, fileContent, 0644)
		if err != nil {
			t.Errorf("Error while writing go stub content to file: %s", err)
		}

	}

	testPluginContent, err := os.ReadFile(testGeneratedGoProtoPath)
	if err != nil {
		t.Errorf("Error while reading proto file")
	}

	protocPluginContent, err := os.ReadFile("testdata/proto/" + testGeneratedGoProtoPath)
	if err != nil {
		t.Errorf("Error while reading proto file")
	}

	if !reflect.DeepEqual(testPluginContent, protocPluginContent) {
		t.Errorf("Error generated go stubs are not equal")
	}

	t.Cleanup(func() {
		os.Remove(testGeneratedGoProtoPath)
	})
}
