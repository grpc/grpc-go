#include "java_generator.h"

#include <iostream>
#include <map>
#include <google/protobuf/compiler/java/java_names.h>
#include <google/protobuf/descriptor.pb.h>
#include <google/protobuf/io/printer.h>
#include <google/protobuf/io/zero_copy_stream.h>

namespace java_grpc_generator {

using google::protobuf::FileDescriptor;
using google::protobuf::ServiceDescriptor;
using google::protobuf::MethodDescriptor;
using google::protobuf::Descriptor;
using google::protobuf::io::Printer;

// Adjust a method name prefix identifier to follow the JavaBean spec:
//   - decapitalize the first letter
//   - remove embedded underscores & capitalize the following letter
static string MixedLower(const string& word) {
  string w;
  w += tolower(word[0]);
  bool after_underscore = false;
  for (size_t i = 1; i < word.length(); ++i) {
    if (word[i] == '_') {
      after_underscore = true;
    } else {
      w += after_underscore ? toupper(word[i]) : word[i];
      after_underscore = false;
    }
  }
  return w;
}

// Converts to the identifier to the ALL_UPPER_CASE format.
//   - An underscore is inserted where a lower case letter is followed by an
//     upper case letter.
//   - All letters are converted to upper case
static string ToAllUpperCase(const string& word) {
  string w;
  for (size_t i = 0; i < word.length(); ++i) {
    w += toupper(word[i]);
    if ((i < word.length() - 1) && islower(word[i]) && isupper(word[i + 1])) {
      w += '_';
    }
  }
  return w;
}

static inline string LowerMethodName(const MethodDescriptor* method) {
  return MixedLower(method->name());
}

static inline string MethodPropertiesFieldName(const MethodDescriptor* method) {
  return "METHOD_" + ToAllUpperCase(method->name());
}

static inline string MessageFullJavaName(bool nano, const Descriptor* desc) {
  string name = google::protobuf::compiler::java::ClassName(desc);
  if (nano && !desc->file()->options().javanano_use_deprecated_package()) {
    // XXX: Add "nano" to the original package
    // (https://github.com/grpc/grpc-java/issues/900)
    for (int i = 0; i < name.size(); ++i) {
      if ((name[i] == '.') && (i < (name.size() - 1)) && isupper(name[i + 1])) {
        return name.substr(0, i + 1) + "nano." + name.substr(i + 1);
      }
    }
  }
  return name;
}

static void PrintMethodFields(
    const ServiceDescriptor* service, map<string, string>* vars, Printer* p,
    bool generate_nano) {
  p->Print("// Static method descriptors that strictly reflect the proto.\n");
  (*vars)["service_name"] = service->name();
  for (int i = 0; i < service->method_count(); ++i) {
    const MethodDescriptor* method = service->method(i);
    (*vars)["method_name"] = method->name();
    (*vars)["input_type"] = MessageFullJavaName(generate_nano,
                                                method->input_type());
    (*vars)["output_type"] = MessageFullJavaName(generate_nano,
                                                 method->output_type());
    (*vars)["method_field_name"] = MethodPropertiesFieldName(method);
    bool client_streaming = method->client_streaming();
    bool server_streaming = method->server_streaming();
    if (client_streaming) {
      if (server_streaming) {
        (*vars)["method_type"] = "BIDI_STREAMING";
      } else {
        (*vars)["method_type"] = "CLIENT_STREAMING";
      }
    } else {
      if (server_streaming) {
        (*vars)["method_type"] = "SERVER_STREAMING";
      } else {
        (*vars)["method_type"] = "UNARY";
      }
    }

    if (generate_nano) {
      // TODO(zsurocking): we're creating two Parsers for each method right now.
      // We could instead create static Parsers and reuse them if some methods
      // share the same request or response messages.
      p->Print(
          *vars,
          "@$ExperimentalApi$\n"
          "public static final $MethodDescriptor$<$input_type$,\n"
          "    $output_type$> $method_field_name$ =\n"
          "    $MethodDescriptor$.create(\n"
          "        $MethodType$.$method_type$,\n"
          "        generateFullMethodName(\n"
          "            \"$Package$$service_name$\", \"$method_name$\"),\n"
          "        $NanoUtils$.<$input_type$>marshaller(\n"
          "            new io.grpc.protobuf.nano.MessageNanoFactory<$input_type$>() {\n"
          "                @Override\n"
          "                public $input_type$ newInstance() {\n"
          "                    return new $input_type$();\n"
          "                }\n"
          "        }),\n"
          "        $NanoUtils$.<$output_type$>marshaller(\n"
          "            new io.grpc.protobuf.nano.MessageNanoFactory<$output_type$>() {\n"
          "                @Override\n"
          "                public $output_type$ newInstance() {\n"
          "                    return new $output_type$();\n"
          "                }\n"
          "        }));\n");
    } else {
      p->Print(
          *vars,
          "@$ExperimentalApi$\n"
          "public static final $MethodDescriptor$<$input_type$,\n"
          "    $output_type$> $method_field_name$ =\n"
          "    $MethodDescriptor$.create(\n"
          "        $MethodType$.$method_type$,\n"
          "        generateFullMethodName(\n"
          "            \"$Package$$service_name$\", \"$method_name$\"),\n"
          "        $ProtoUtils$.marshaller($input_type$.getDefaultInstance()),\n"
          "        $ProtoUtils$.marshaller($output_type$.getDefaultInstance()));\n");
    }
  }
  p->Print("\n");
}

enum StubType {
  ASYNC_INTERFACE = 0,
  BLOCKING_CLIENT_INTERFACE = 1,
  FUTURE_CLIENT_INTERFACE = 2,
  BLOCKING_SERVER_INTERFACE = 3,
  ASYNC_CLIENT_IMPL = 4,
  BLOCKING_CLIENT_IMPL = 5,
  FUTURE_CLIENT_IMPL = 6
};

enum CallType {
  ASYNC_CALL = 0,
  BLOCKING_CALL = 1,
  FUTURE_CALL = 2
};

// Prints a client interface or implementation class, or a server interface.
static void PrintStub(
    const ServiceDescriptor* service,
    map<string, string>* vars,
    Printer* p, StubType type, bool generate_nano) {
  (*vars)["service_name"] = service->name();
  string interface_name = service->name();
  string impl_name = service->name();
  switch (type) {
    case ASYNC_INTERFACE:
    case ASYNC_CLIENT_IMPL:
      impl_name += "Stub";
      break;
    case BLOCKING_CLIENT_INTERFACE:
    case BLOCKING_CLIENT_IMPL:
      interface_name += "BlockingClient";
      impl_name += "BlockingStub";
      break;
    case FUTURE_CLIENT_INTERFACE:
    case FUTURE_CLIENT_IMPL:
      interface_name += "FutureClient";
      impl_name += "FutureStub";
      break;
    case BLOCKING_SERVER_INTERFACE:
      interface_name += "BlockingServer";
      break;
    default:
      GRPC_CODEGEN_FAIL << "Cannot determine class name for StubType: " << type;
  }
  bool impl;
  CallType call_type;
  switch (type) {
    case ASYNC_INTERFACE:
      call_type = ASYNC_CALL;
      impl = false;
      break;
    case BLOCKING_CLIENT_INTERFACE:
    case BLOCKING_SERVER_INTERFACE:
      call_type = BLOCKING_CALL;
      impl = false;
      break;
    case FUTURE_CLIENT_INTERFACE:
      call_type = FUTURE_CALL;
      impl = false;
      break;
    case ASYNC_CLIENT_IMPL:
      call_type = ASYNC_CALL;
      impl = true;
      break;
    case BLOCKING_CLIENT_IMPL:
      call_type = BLOCKING_CALL;
      impl = true;
      break;
    case FUTURE_CLIENT_IMPL:
      call_type = FUTURE_CALL;
      impl = true;
      break;
    default:
      GRPC_CODEGEN_FAIL << "Cannot determine call type for StubType: " << type;
  }
  (*vars)["interface_name"] = interface_name;
  (*vars)["impl_name"] = impl_name;

  // Class head
  if (!impl) {
    p->Print(
        *vars,
        "public static interface $interface_name$ {\n");
  } else {
    p->Print(
        *vars,
        "public static class $impl_name$ extends $AbstractStub$<$impl_name$>\n"
        "    implements $interface_name$ {\n");
  }
  p->Indent();

  // Constructor and build() method
  if (impl) {
    p->Print(
        *vars,
        "private $impl_name$($Channel$ channel) {\n");
    p->Indent();
    p->Print("super(channel);\n");
    p->Outdent();
    p->Print("}\n\n");
    p->Print(
        *vars,
        "private $impl_name$($Channel$ channel,\n"
        "    $CallOptions$ callOptions) {\n");
    p->Indent();
    p->Print("super(channel, callOptions);\n");
    p->Outdent();
    p->Print("}\n\n");
    p->Print(
        *vars,
        "@$Override$\n"
        "protected $impl_name$ build($Channel$ channel,\n"
        "    $CallOptions$ callOptions) {\n");
    p->Indent();
    p->Print(
        *vars,
        "return new $impl_name$(channel, callOptions);\n");
    p->Outdent();
    p->Print("}\n");
  }

  // RPC methods
  for (int i = 0; i < service->method_count(); ++i) {
    const MethodDescriptor* method = service->method(i);
    (*vars)["input_type"] = MessageFullJavaName(generate_nano,
                                                method->input_type());
    (*vars)["output_type"] = MessageFullJavaName(generate_nano,
                                                 method->output_type());
    (*vars)["lower_method_name"] = LowerMethodName(method);
    (*vars)["method_field_name"] = MethodPropertiesFieldName(method);
    bool client_streaming = method->client_streaming();
    bool server_streaming = method->server_streaming();

    if (call_type == BLOCKING_CALL && client_streaming) {
      // Blocking client interface with client streaming is not available
      continue;
    }

    if (call_type == FUTURE_CALL && (client_streaming || server_streaming)) {
      // Future interface doesn't support streaming.
      continue;
    }

    // Method signature
    p->Print("\n");
    if (impl) {
      p->Print(
          *vars,
          "@$Override$\n");
    }
    p->Print("public ");
    switch (call_type) {
      case BLOCKING_CALL:
        // TODO(zhangkun83): decide the blocking server interface
        GRPC_CODEGEN_CHECK(type != BLOCKING_SERVER_INTERFACE)
            << "Blocking server interface is not available";
        GRPC_CODEGEN_CHECK(!client_streaming)
            << "Blocking client interface with client streaming is unavailable";
        if (server_streaming) {
          // Server streaming
          p->Print(
              *vars,
              "$Iterator$<$output_type$> $lower_method_name$(\n"
              "    $input_type$ request)");
        } else {
          // Simple RPC
          p->Print(
              *vars,
              "$output_type$ $lower_method_name$($input_type$ request)");
        }
        break;
      case ASYNC_CALL:
        if (client_streaming) {
          // Bidirectional streaming or client streaming
          p->Print(
              *vars,
              "$StreamObserver$<$input_type$> $lower_method_name$(\n"
              "    $StreamObserver$<$output_type$> responseObserver)");
        } else {
          // Server streaming or simple RPC
          p->Print(
              *vars,
              "void $lower_method_name$($input_type$ request,\n"
              "    $StreamObserver$<$output_type$> responseObserver)");
        }
        break;
      case FUTURE_CALL:
        GRPC_CODEGEN_CHECK(!client_streaming && !server_streaming)
            << "Future interface doesn't support streaming. "
            << "client_streaming=" << client_streaming << ", "
            << "server_streaming=" << server_streaming;
        p->Print(
            *vars,
            "$ListenableFuture$<$output_type$> $lower_method_name$(\n"
            "    $input_type$ request)");
        break;
    }
    if (impl) {
      // Method body for client impls
      p->Print(" {\n");
      p->Indent();
      switch (call_type) {
        case BLOCKING_CALL:
          GRPC_CODEGEN_CHECK(!client_streaming)
              << "Blocking client streaming interface is not available";
          if (server_streaming) {
            (*vars)["calls_method"] = "blockingServerStreamingCall";
            (*vars)["params"] = "request";
          } else {
            (*vars)["calls_method"] = "blockingUnaryCall";
            (*vars)["params"] = "request";
          }
          p->Print(
              *vars,
              "return $calls_method$(\n"
              "    getChannel().newCall($method_field_name$, getCallOptions()), $params$);\n");
          break;
        case ASYNC_CALL:
          if (server_streaming) {
            if (client_streaming) {
              (*vars)["calls_method"] = "asyncBidiStreamingCall";
              (*vars)["params"] = "responseObserver";
            } else {
              (*vars)["calls_method"] = "asyncServerStreamingCall";
              (*vars)["params"] = "request, responseObserver";
            }
          } else {
            if (client_streaming) {
              (*vars)["calls_method"] = "asyncClientStreamingCall";
              (*vars)["params"] = "responseObserver";
            } else {
              (*vars)["calls_method"] = "asyncUnaryCall";
              (*vars)["params"] = "request, responseObserver";
            }
          }
          (*vars)["last_line_prefix"] = client_streaming ? "return " : "";
          p->Print(
              *vars,
              "$last_line_prefix$$calls_method$(\n"
              "    getChannel().newCall($method_field_name$, getCallOptions()), $params$);\n");
          break;
        case FUTURE_CALL:
          GRPC_CODEGEN_CHECK(!client_streaming && !server_streaming)
              << "Future interface doesn't support streaming. "
              << "client_streaming=" << client_streaming << ", "
              << "server_streaming=" << server_streaming;
          (*vars)["calls_method"] = "futureUnaryCall";
          p->Print(
              *vars,
              "return $calls_method$(\n"
              "    getChannel().newCall($method_field_name$, getCallOptions()), request);\n");
          break;
      }
      p->Outdent();
      p->Print("}\n");
    } else {
      p->Print(";\n");
    }
  }
  p->Outdent();
  p->Print("}\n\n");
}

static void PrintBindServiceMethod(const ServiceDescriptor* service,
                                   map<string, string>* vars,
                                   Printer* p,
                                   bool generate_nano) {
  (*vars)["service_name"] = service->name();
  p->Print(
      *vars,
      "public static $ServerServiceDefinition$ bindService(\n"
      "    final $service_name$ serviceImpl) {\n");
  p->Indent();
  p->Print(*vars,
           "return "
           "$ServerServiceDefinition$.builder(SERVICE_NAME)\n");
  p->Indent();
  for (int i = 0; i < service->method_count(); ++i) {
    const MethodDescriptor* method = service->method(i);
    (*vars)["lower_method_name"] = LowerMethodName(method);
    (*vars)["method_field_name"] = MethodPropertiesFieldName(method);
    (*vars)["input_type"] = MessageFullJavaName(generate_nano,
                                                method->input_type());
    (*vars)["output_type"] = MessageFullJavaName(generate_nano,
                                                 method->output_type());
    bool client_streaming = method->client_streaming();
    bool server_streaming = method->server_streaming();
    if (client_streaming) {
      if (server_streaming) {
        (*vars)["calls_method"] = "asyncBidiStreamingCall";
        (*vars)["invocation_class"] =
            "io.grpc.stub.ServerCalls.BidiStreamingMethod";
      } else {
        (*vars)["calls_method"] = "asyncClientStreamingCall";
        (*vars)["invocation_class"] =
            "io.grpc.stub.ServerCalls.ClientStreamingMethod";
      }
    } else {
      if (server_streaming) {
        (*vars)["calls_method"] = "asyncServerStreamingCall";
        (*vars)["invocation_class"] =
            "io.grpc.stub.ServerCalls.ServerStreamingMethod";
      } else {
        (*vars)["calls_method"] = "asyncUnaryCall";
        (*vars)["invocation_class"] =
            "io.grpc.stub.ServerCalls.UnaryMethod";
      }
    }
    p->Print(*vars, ".addMethod(\n");
    p->Indent();
    p->Print(
        *vars,
        "$method_field_name$,\n"
        "$calls_method$(\n");
    p->Indent();
    p->Print(
        *vars,
        "new $invocation_class$<\n"
        "    $input_type$,\n"
        "    $output_type$>() {\n");
    p->Indent();
    p->Print(
        *vars,
        "@$Override$\n");
    if (client_streaming) {
      p->Print(
          *vars,
          "public $StreamObserver$<$input_type$> invoke(\n"
          "    $StreamObserver$<$output_type$> responseObserver) {\n"
          "  return serviceImpl.$lower_method_name$(responseObserver);\n"
          "}\n");
    } else {
      p->Print(
          *vars,
          "public void invoke(\n"
          "    $input_type$ request,\n"
          "    $StreamObserver$<$output_type$> responseObserver) {\n"
          "  serviceImpl.$lower_method_name$(request, responseObserver);\n"
          "}\n");
    }
    p->Outdent();
    p->Print("}))");
    if (i == service->method_count() - 1) {
      p->Print(".build();");
    }
    p->Print("\n");
    p->Outdent();
    p->Outdent();
  }
  p->Outdent();
  p->Outdent();
  p->Print("}\n");
}

static void PrintService(const ServiceDescriptor* service,
                         map<string, string>* vars,
                         Printer* p,
                         bool generate_nano) {
  (*vars)["service_name"] = service->name();
  (*vars)["service_class_name"] = ServiceClassName(service);
  p->Print(
      *vars,
      "@$Generated$(\"by gRPC proto compiler\")\n"
      "public class $service_class_name$ {\n\n");
  p->Indent();
  p->Print(
      *vars,
      "private $service_class_name$() {}\n\n");

  p->Print(
      *vars,
      "public static final String SERVICE_NAME = "
      "\"$Package$$service_name$\";\n\n");

  PrintMethodFields(service, vars, p, generate_nano);

  p->Print(
      *vars,
      "public static $service_name$Stub newStub($Channel$ channel) {\n");
  p->Indent();
  p->Print(
      *vars,
      "return new $service_name$Stub(channel);\n");
  p->Outdent();
  p->Print("}\n\n");
  p->Print(
      *vars,
      "public static $service_name$BlockingStub newBlockingStub(\n"
      "    $Channel$ channel) {\n");
  p->Indent();
  p->Print(
      *vars,
      "return new $service_name$BlockingStub(channel);\n");
  p->Outdent();
  p->Print("}\n\n");
  p->Print(
      *vars,
      "public static $service_name$FutureStub newFutureStub(\n"
      "    $Channel$ channel) {\n");
  p->Indent();
  p->Print(
      *vars,
      "return new $service_name$FutureStub(channel);\n");
  p->Outdent();
  p->Print("}\n\n");

  PrintStub(service, vars, p, ASYNC_INTERFACE, generate_nano);
  PrintStub(service, vars, p, BLOCKING_CLIENT_INTERFACE, generate_nano);
  PrintStub(service, vars, p, FUTURE_CLIENT_INTERFACE, generate_nano);
  PrintStub(service, vars, p, ASYNC_CLIENT_IMPL, generate_nano);
  PrintStub(service, vars, p, BLOCKING_CLIENT_IMPL, generate_nano);
  PrintStub(service, vars, p, FUTURE_CLIENT_IMPL, generate_nano);
  PrintBindServiceMethod(service, vars, p, generate_nano);
  p->Outdent();
  p->Print("}\n");
}

void PrintImports(Printer* p, bool generate_nano) {
  p->Print(
      "import static "
      "io.grpc.stub.ClientCalls.asyncUnaryCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.asyncServerStreamingCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.asyncClientStreamingCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.asyncBidiStreamingCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.blockingUnaryCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.blockingServerStreamingCall;\n"
      "import static "
      "io.grpc.stub.ClientCalls.futureUnaryCall;\n"
      "import static "
      "io.grpc.MethodDescriptor.generateFullMethodName;\n"
      "import static "
      "io.grpc.stub.ServerCalls.asyncUnaryCall;\n"
      "import static "
      "io.grpc.stub.ServerCalls.asyncServerStreamingCall;\n"
      "import static "
      "io.grpc.stub.ServerCalls.asyncClientStreamingCall;\n"
      "import static "
      "io.grpc.stub.ServerCalls.asyncBidiStreamingCall;\n\n");
  if (generate_nano) {
    p->Print("import java.io.IOException;\n\n");
  }
}

void GenerateService(const ServiceDescriptor* service,
                     google::protobuf::io::ZeroCopyOutputStream* out,
                     bool generate_nano) {
  // All non-generated classes must be referred by fully qualified names to
  // avoid collision with generated classes.
  map<string, string> vars;
  vars["String"] = "java.lang.String";
  vars["Override"] = "java.lang.Override";
  vars["Channel"] = "io.grpc.Channel";
  vars["CallOptions"] = "io.grpc.CallOptions";
  vars["MethodType"] = "io.grpc.MethodDescriptor.MethodType";
  vars["ServerMethodDefinition"] =
      "io.grpc.ServerMethodDefinition";
  vars["ServerServiceDefinition"] =
      "io.grpc.ServerServiceDefinition";
  vars["AbstractStub"] = "io.grpc.stub.AbstractStub";
  vars["ImmutableList"] = "com.google.common.collect.ImmutableList";
  vars["Collection"] = "java.util.Collection";
  vars["MethodDescriptor"] = "io.grpc.MethodDescriptor";
  vars["ProtoUtils"] = "io.grpc.protobuf.ProtoUtils";
  vars["NanoUtils"] = "io.grpc.protobuf.nano.NanoUtils";
  vars["StreamObserver"] = "io.grpc.stub.StreamObserver";
  vars["Iterator"] = "java.util.Iterator";
  vars["Map"] = "java.util.Map";
  vars["TimeUnit"] = "java.util.concurrent.TimeUnit";
  vars["Generated"] = "javax.annotation.Generated";
  vars["Immutable"] = "javax.annotation.concurrent.Immutable";
  vars["ListenableFuture"] =
      "com.google.common.util.concurrent.ListenableFuture";
  vars["ExperimentalApi"] = "io.grpc.ExperimentalApi";

  Printer printer(out, '$');
  string package_name = ServiceJavaPackage(service->file());
  printer.Print(
      "package $package_name$;\n\n",
      "package_name", package_name);
  PrintImports(&printer, generate_nano);

  // Package string is used to fully qualify method names.
  vars["Package"] = service->file()->package();
  if (!vars["Package"].empty()) {
    vars["Package"].append(".");
  }
  PrintService(service, &vars, &printer, generate_nano);
}

string ServiceJavaPackage(const FileDescriptor* file) {
  string result = google::protobuf::compiler::java::ClassName(file);
  size_t last_dot_pos = result.find_last_of('.');
  if (last_dot_pos != string::npos) {
    result.resize(last_dot_pos);
  }
  return result;
}

string ServiceClassName(const google::protobuf::ServiceDescriptor* service) {
  return service->name() + "Grpc";
}

}  // namespace java_grpc_generator
