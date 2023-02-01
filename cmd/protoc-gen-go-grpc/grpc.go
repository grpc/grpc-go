/*
 *
 * Copyright 2020 gRPC authors.
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
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	contextPackage = protogen.GoImportPath("context")
	grpcPackage    = protogen.GoImportPath("google.golang.org/grpc")
	codesPackage   = protogen.GoImportPath("google.golang.org/grpc/codes")
	statusPackage  = protogen.GoImportPath("google.golang.org/grpc/status")
)

type serviceGenerateHelperInterface interface {
	formatFullMethodSymbol(service *protogen.Service, method *protogen.Method) string
	genFullMethods(g *protogen.GeneratedFile, service *protogen.Service)
	generateClientStruct(g *protogen.GeneratedFile, clientName string)
	generateNewClientDefinitions(g *protogen.GeneratedFile, service *protogen.Service, clientName string)
	generateUnimplementedServerType(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service)
	generateServerFunctions(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service, serverType string, serviceDescVar string)
	formatHandlerFuncName(service *protogen.Service, hname string) string
}

type serviceGenerateHelper struct{}

func (serviceGenerateHelper) formatFullMethodSymbol(service *protogen.Service, method *protogen.Method) string {
	return fmt.Sprintf("%s_%s_FullMethodName", service.GoName, method.GoName)
}

func (serviceGenerateHelper) genFullMethods(g *protogen.GeneratedFile, service *protogen.Service) {
	g.P("const (")
	for _, method := range service.Methods {
		fmSymbol := helper.formatFullMethodSymbol(service, method)
		fmName := fmt.Sprintf("/%s/%s", service.Desc.FullName(), method.Desc.Name())
		g.P(fmSymbol, ` = "`, fmName, `"`)
	}
	g.P(")")
	g.P()
}

func (serviceGenerateHelper) generateClientStruct(g *protogen.GeneratedFile, clientName string) {
	g.P("type ", unexport(clientName), " struct {")
	g.P("cc ", grpcPackage.Ident("ClientConnInterface"))
	g.P("}")
	g.P()
}

func (serviceGenerateHelper) generateNewClientDefinitions(g *protogen.GeneratedFile, service *protogen.Service, clientName string) {
	g.P("return &", unexport(clientName), "{cc}")
}

func (serviceGenerateHelper) generateUnimplementedServerType(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service) {
	serverType := service.GoName + "Server"
	mustOrShould := "must"
	if !*requireUnimplemented {
		mustOrShould = "should"
	}
	// Server Unimplemented struct for forward compatibility.
	g.P("// Unimplemented", serverType, " ", mustOrShould, " be embedded to have forward compatible implementations.")
	g.P("type Unimplemented", serverType, " struct {")
	g.P("}")
	g.P()
	for _, method := range service.Methods {
		nilArg := ""
		if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
			nilArg = "nil,"
		}
		g.P("func (Unimplemented", serverType, ") ", serverSignature(g, method), "{")
		g.P("return ", nilArg, statusPackage.Ident("Errorf"), "(", codesPackage.Ident("Unimplemented"), `, "method `, method.GoName, ` not implemented")`)
		g.P("}")
	}
	if *requireUnimplemented {
		g.P("func (Unimplemented", serverType, ") mustEmbedUnimplemented", serverType, "() {}")
	}
	g.P()
}

func (serviceGenerateHelper) generateServerFunctions(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service, serverType string, serviceDescVar string) {
	// Server handler implementations.
	handlerNames := make([]string, 0, len(service.Methods))
	for _, method := range service.Methods {
		hname := genServerMethod(gen, file, g, method, func(hname string) string {
			return hname
		})
		handlerNames = append(handlerNames, hname)
	}
	genServiceDesc(file, g, serviceDescVar, serverType, service, handlerNames)
}

func (serviceGenerateHelper) formatHandlerFuncName(service *protogen.Service, hname string) string {
	return hname
}

var helper serviceGenerateHelperInterface = serviceGenerateHelper{}

// FileDescriptorProto.package field number
const fileDescriptorProtoPackageFieldNumber = 2

// FileDescriptorProto.syntax field number
const fileDescriptorProtoSyntaxFieldNumber = 12

// generateFile generates a _grpc.pb.go file containing gRPC service definitions.
func generateFile(gen *protogen.Plugin, file *protogen.File) *protogen.GeneratedFile {
	if len(file.Services) == 0 {
		return nil
	}
	filename := file.GeneratedFilenamePrefix + "_grpc.pb.go"
	g := gen.NewGeneratedFile(filename, file.GoImportPath)
	// Attach all comments associated with the syntax field.
	genLeadingComments(g, file.Desc.SourceLocations().ByPath(protoreflect.SourcePath{fileDescriptorProtoSyntaxFieldNumber}))
	g.P("// Code generated by protoc-gen-go-grpc. DO NOT EDIT.")
	g.P("// versions:")
	g.P("// - protoc-gen-go-grpc v", version)
	g.P("// - protoc             ", protocVersion(gen))
	if file.Proto.GetOptions().GetDeprecated() {
		g.P("// ", file.Desc.Path(), " is a deprecated file.")
	} else {
		g.P("// source: ", file.Desc.Path())
	}
	g.P()
	// Attach all comments associated with the package field.
	genLeadingComments(g, file.Desc.SourceLocations().ByPath(protoreflect.SourcePath{fileDescriptorProtoPackageFieldNumber}))
	g.P("package ", file.GoPackageName)
	g.P()
	generateFileContent(gen, file, g)
	return g
}

func protocVersion(gen *protogen.Plugin) string {
	v := gen.Request.GetCompilerVersion()
	if v == nil {
		return "(unknown)"
	}
	var suffix string
	if s := v.GetSuffix(); s != "" {
		suffix = "-" + s
	}
	return fmt.Sprintf("v%d.%d.%d%s", v.GetMajor(), v.GetMinor(), v.GetPatch(), suffix)
}

// generateFileContent generates the gRPC service definitions, excluding the package statement.
func generateFileContent(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile) {
	if len(file.Services) == 0 {
		return
	}

	g.P("// This is a compile-time assertion to ensure that this generated file")
	g.P("// is compatible with the grpc package it is being compiled against.")
	g.P("// Requires gRPC-Go v1.32.0 or later.")
	g.P("const _ = ", grpcPackage.Ident("SupportPackageIsVersion7")) // When changing, update version number above.
	g.P()
	for _, service := range file.Services {
		genService(gen, file, g, service)
	}
}

func genService(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, service *protogen.Service) {
	// Full methods constants.
	helper.genFullMethods(g, service)

	// Client interface.
	clientName := service.GoName + "Client"

	g.P("// ", clientName, " is the client API for ", service.GoName, " service.")
	g.P("//")
	g.P("// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.")

	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		g.P(deprecationComment)
	}
	g.Annotate(clientName, service.Location)
	g.P("type ", clientName, " interface {")
	for _, method := range service.Methods {
		g.Annotate(clientName+"."+method.GoName, method.Location)
		if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
			g.P(deprecationComment)
		}
		g.P(method.Comments.Leading,
			clientSignature(g, method))
	}
	g.P("}")
	g.P()

	// Client structure.
	helper.generateClientStruct(g, clientName)

	// NewClient factory.
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P(deprecationComment)
	}
	g.P("func New", clientName, " (cc ", grpcPackage.Ident("ClientConnInterface"), ") ", clientName, " {")
	helper.generateNewClientDefinitions(g, service, clientName)
	g.P("}")
	g.P()

	var methodIndex, streamIndex int
	// Client method implementations.
	for _, method := range service.Methods {
		if !method.Desc.IsStreamingServer() && !method.Desc.IsStreamingClient() {
			// Unary RPC method
			genClientMethod(gen, file, g, method, methodIndex)
			methodIndex++
		} else {
			// Streaming RPC method
			genClientMethod(gen, file, g, method, streamIndex)
			streamIndex++
		}
	}

	mustOrShould := "must"
	if !*requireUnimplemented {
		mustOrShould = "should"
	}

	// Server interface.
	serverType := service.GoName + "Server"
	g.P("// ", serverType, " is the server API for ", service.GoName, " service.")
	g.P("// All implementations ", mustOrShould, " embed Unimplemented", serverType)
	g.P("// for forward compatibility")
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P("//")
		g.P(deprecationComment)
	}
	g.Annotate(serverType, service.Location)
	g.P("type ", serverType, " interface {")
	for _, method := range service.Methods {
		g.Annotate(serverType+"."+method.GoName, method.Location)
		if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
			g.P(deprecationComment)
		}
		g.P(method.Comments.Leading,
			serverSignature(g, method))
	}
	if *requireUnimplemented {
		g.P("mustEmbedUnimplemented", serverType, "()")
	}
	g.P("}")
	g.P()

	// Server Unimplemented struct for forward compatibility.
	helper.generateUnimplementedServerType(gen, file, g, service)

	// Unsafe Server interface to opt-out of forward compatibility.
	g.P("// Unsafe", serverType, " may be embedded to opt out of forward compatibility for this service.")
	g.P("// Use of this interface is not recommended, as added methods to ", serverType, " will")
	g.P("// result in compilation errors.")
	g.P("type Unsafe", serverType, " interface {")
	g.P("mustEmbedUnimplemented", serverType, "()")
	g.P("}")

	// Server registration.
	if service.Desc.Options().(*descriptorpb.ServiceOptions).GetDeprecated() {
		g.P(deprecationComment)
	}
	serviceDescVar := service.GoName + "_ServiceDesc"
	g.P("func Register", service.GoName, "Server(s ", grpcPackage.Ident("ServiceRegistrar"), ", srv ", serverType, ") {")
	g.P("s.RegisterService(&", serviceDescVar, `, srv)`)
	g.P("}")
	g.P()

	helper.generateServerFunctions(gen, file, g, service, serverType, serviceDescVar)
}

func clientSignature(g *protogen.GeneratedFile, method *protogen.Method) string {
	s := method.GoName + "(ctx " + g.QualifiedGoIdent(contextPackage.Ident("Context"))
	if !method.Desc.IsStreamingClient() {
		s += ", in *" + g.QualifiedGoIdent(method.Input.GoIdent)
	}
	s += ", opts ..." + g.QualifiedGoIdent(grpcPackage.Ident("CallOption")) + ") ("
	if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
		s += "*" + g.QualifiedGoIdent(method.Output.GoIdent)
	} else {
		s += method.Parent.GoName + "_" + method.GoName + "Client"
	}
	s += ", error)"
	return s
}

func genClientMethod(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, method *protogen.Method, index int) {
	service := method.Parent
	fmSymbol := helper.formatFullMethodSymbol(service, method)

	if method.Desc.Options().(*descriptorpb.MethodOptions).GetDeprecated() {
		g.P(deprecationComment)
	}
	g.P("func (c *", unexport(service.GoName), "Client) ", clientSignature(g, method), "{")
	if !method.Desc.IsStreamingServer() && !method.Desc.IsStreamingClient() {
		g.P("out := new(", method.Output.GoIdent, ")")
		g.P(`err := c.cc.Invoke(ctx, `, fmSymbol, `, in, out, opts...)`)
		g.P("if err != nil { return nil, err }")
		g.P("return out, nil")
		g.P("}")
		g.P()
		return
	}
	streamType := unexport(service.GoName) + method.GoName + "Client"
	serviceDescVar := service.GoName + "_ServiceDesc"
	g.P("stream, err := c.cc.NewStream(ctx, &", serviceDescVar, ".Streams[", index, `], `, fmSymbol, `, opts...)`)
	g.P("if err != nil { return nil, err }")
	g.P("x := &", streamType, "{stream}")
	if !method.Desc.IsStreamingClient() {
		g.P("if err := x.ClientStream.SendMsg(in); err != nil { return nil, err }")
		g.P("if err := x.ClientStream.CloseSend(); err != nil { return nil, err }")
	}
	g.P("return x, nil")
	g.P("}")
	g.P()

	genSend := method.Desc.IsStreamingClient()
	genRecv := method.Desc.IsStreamingServer()
	genCloseAndRecv := !method.Desc.IsStreamingServer()

	// Stream auxiliary types and methods.
	g.P("type ", service.GoName, "_", method.GoName, "Client interface {")
	if genSend {
		g.P("Send(*", method.Input.GoIdent, ") error")
	}
	if genRecv {
		g.P("Recv() (*", method.Output.GoIdent, ", error)")
	}
	if genCloseAndRecv {
		g.P("CloseAndRecv() (*", method.Output.GoIdent, ", error)")
	}
	g.P(grpcPackage.Ident("ClientStream"))
	g.P("}")
	g.P()

	g.P("type ", streamType, " struct {")
	g.P(grpcPackage.Ident("ClientStream"))
	g.P("}")
	g.P()

	if genSend {
		g.P("func (x *", streamType, ") Send(m *", method.Input.GoIdent, ") error {")
		g.P("return x.ClientStream.SendMsg(m)")
		g.P("}")
		g.P()
	}
	if genRecv {
		g.P("func (x *", streamType, ") Recv() (*", method.Output.GoIdent, ", error) {")
		g.P("m := new(", method.Output.GoIdent, ")")
		g.P("if err := x.ClientStream.RecvMsg(m); err != nil { return nil, err }")
		g.P("return m, nil")
		g.P("}")
		g.P()
	}
	if genCloseAndRecv {
		g.P("func (x *", streamType, ") CloseAndRecv() (*", method.Output.GoIdent, ", error) {")
		g.P("if err := x.ClientStream.CloseSend(); err != nil { return nil, err }")
		g.P("m := new(", method.Output.GoIdent, ")")
		g.P("if err := x.ClientStream.RecvMsg(m); err != nil { return nil, err }")
		g.P("return m, nil")
		g.P("}")
		g.P()
	}
}

func serverSignature(g *protogen.GeneratedFile, method *protogen.Method) string {
	var reqArgs []string
	ret := "error"
	if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
		reqArgs = append(reqArgs, g.QualifiedGoIdent(contextPackage.Ident("Context")))
		ret = "(*" + g.QualifiedGoIdent(method.Output.GoIdent) + ", error)"
	}
	if !method.Desc.IsStreamingClient() {
		reqArgs = append(reqArgs, "*"+g.QualifiedGoIdent(method.Input.GoIdent))
	}
	if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
		reqArgs = append(reqArgs, method.Parent.GoName+"_"+method.GoName+"Server")
	}
	return method.GoName + "(" + strings.Join(reqArgs, ", ") + ") " + ret
}

func genServiceDesc(file *protogen.File, g *protogen.GeneratedFile, serviceDescVar string, serverType string, service *protogen.Service, handlerNames []string) {
	// Service descriptor.
	g.P("// ", serviceDescVar, " is the ", grpcPackage.Ident("ServiceDesc"), " for ", service.GoName, " service.")
	g.P("// It's only intended for direct use with ", grpcPackage.Ident("RegisterService"), ",")
	g.P("// and not to be introspected or modified (even as a copy)")
	g.P("var ", serviceDescVar, " = ", grpcPackage.Ident("ServiceDesc"), " {")
	g.P("ServiceName: ", strconv.Quote(string(service.Desc.FullName())), ",")
	g.P("HandlerType: (*", serverType, ")(nil),")
	g.P("Methods: []", grpcPackage.Ident("MethodDesc"), "{")
	for i, method := range service.Methods {
		if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
			continue
		}
		g.P("{")
		g.P("MethodName: ", strconv.Quote(string(method.Desc.Name())), ",")
		g.P("Handler: ", handlerNames[i], ",")
		g.P("},")
	}
	g.P("},")
	g.P("Streams: []", grpcPackage.Ident("StreamDesc"), "{")
	for i, method := range service.Methods {
		if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
			continue
		}
		g.P("{")
		g.P("StreamName: ", strconv.Quote(string(method.Desc.Name())), ",")
		g.P("Handler: ", handlerNames[i], ",")
		if method.Desc.IsStreamingServer() {
			g.P("ServerStreams: true,")
		}
		if method.Desc.IsStreamingClient() {
			g.P("ClientStreams: true,")
		}
		g.P("},")
	}
	g.P("},")
	g.P("Metadata: \"", file.Desc.Path(), "\",")
	g.P("}")
	g.P()
}

func genServerMethod(gen *protogen.Plugin, file *protogen.File, g *protogen.GeneratedFile, method *protogen.Method, hnameFuncNameFormatter func(string) string) string {
	service := method.Parent
	hname := fmt.Sprintf("_%s_%s_Handler", service.GoName, method.GoName)

	if !method.Desc.IsStreamingClient() && !method.Desc.IsStreamingServer() {
		g.P("func ", hnameFuncNameFormatter(hname), "(srv interface{}, ctx ", contextPackage.Ident("Context"), ", dec func(interface{}) error, interceptor ", grpcPackage.Ident("UnaryServerInterceptor"), ") (interface{}, error) {")
		g.P("in := new(", method.Input.GoIdent, ")")
		g.P("if err := dec(in); err != nil { return nil, err }")
		g.P("if interceptor == nil { return srv.(", service.GoName, "Server).", method.GoName, "(ctx, in) }")
		g.P("info := &", grpcPackage.Ident("UnaryServerInfo"), "{")
		g.P("Server: srv,")
		fmSymbol := helper.formatFullMethodSymbol(service, method)
		g.P("FullMethod: ", fmSymbol, ",")
		g.P("}")
		g.P("handler := func(ctx ", contextPackage.Ident("Context"), ", req interface{}) (interface{}, error) {")
		g.P("return srv.(", service.GoName, "Server).", method.GoName, "(ctx, req.(*", method.Input.GoIdent, "))")
		g.P("}")
		g.P("return interceptor(ctx, in, info, handler)")
		g.P("}")
		g.P()
		return hname
	}
	streamType := unexport(service.GoName) + method.GoName + "Server"
	g.P("func ", hnameFuncNameFormatter(hname), "(srv interface{}, stream ", grpcPackage.Ident("ServerStream"), ") error {")
	if !method.Desc.IsStreamingClient() {
		g.P("m := new(", method.Input.GoIdent, ")")
		g.P("if err := stream.RecvMsg(m); err != nil { return err }")
		g.P("return srv.(", service.GoName, "Server).", method.GoName, "(m, &", streamType, "{stream})")
	} else {
		g.P("return srv.(", service.GoName, "Server).", method.GoName, "(&", streamType, "{stream})")
	}
	g.P("}")
	g.P()

	genSend := method.Desc.IsStreamingServer()
	genSendAndClose := !method.Desc.IsStreamingServer()
	genRecv := method.Desc.IsStreamingClient()

	// Stream auxiliary types and methods.
	g.P("type ", service.GoName, "_", method.GoName, "Server interface {")
	if genSend {
		g.P("Send(*", method.Output.GoIdent, ") error")
	}
	if genSendAndClose {
		g.P("SendAndClose(*", method.Output.GoIdent, ") error")
	}
	if genRecv {
		g.P("Recv() (*", method.Input.GoIdent, ", error)")
	}
	g.P(grpcPackage.Ident("ServerStream"))
	g.P("}")
	g.P()

	g.P("type ", streamType, " struct {")
	g.P(grpcPackage.Ident("ServerStream"))
	g.P("}")
	g.P()

	if genSend {
		g.P("func (x *", streamType, ") Send(m *", method.Output.GoIdent, ") error {")
		g.P("return x.ServerStream.SendMsg(m)")
		g.P("}")
		g.P()
	}
	if genSendAndClose {
		g.P("func (x *", streamType, ") SendAndClose(m *", method.Output.GoIdent, ") error {")
		g.P("return x.ServerStream.SendMsg(m)")
		g.P("}")
		g.P()
	}
	if genRecv {
		g.P("func (x *", streamType, ") Recv() (*", method.Input.GoIdent, ", error) {")
		g.P("m := new(", method.Input.GoIdent, ")")
		g.P("if err := x.ServerStream.RecvMsg(m); err != nil { return nil, err }")
		g.P("return m, nil")
		g.P("}")
		g.P()
	}

	return hname
}

func genLeadingComments(g *protogen.GeneratedFile, loc protoreflect.SourceLocation) {
	for _, s := range loc.LeadingDetachedComments {
		g.P(protogen.Comments(s))
		g.P()
	}
	if s := loc.LeadingComments; s != "" {
		g.P(protogen.Comments(s))
		g.P()
	}
}

const deprecationComment = "// Deprecated: Do not use."

func unexport(s string) string { return strings.ToLower(s[:1]) + s[1:] }
