// Generates Java gRPC service interface out of Protobuf IDL.
//
// This is a Proto2 compiler plugin.  See net/proto2/compiler/proto/plugin.proto
// and net/proto2/compiler/public/plugin.h for more information on plugins.

#include <memory>

#include "java_generator.h"
#include <google/protobuf/compiler/code_generator.h>
#include <google/protobuf/compiler/plugin.h>
#include <google/protobuf/io/zero_copy_stream.h>
#include <google/protobuf/descriptor.h>

static string JavaPackageToDir(const string& package_name) {
  string package_dir = package_name;
  for (size_t i = 0; i < package_dir.size(); ++i) {
    if (package_dir[i] == '.') {
      package_dir[i] = '/';
    }
  }
  if (!package_dir.empty()) package_dir += "/";
  return package_dir;
}

class JavaGrpcGenerator : public google::protobuf::compiler::CodeGenerator {
 public:
  JavaGrpcGenerator() {}
  virtual ~JavaGrpcGenerator() {}

  virtual bool Generate(const google::protobuf::FileDescriptor* file,
                        const string& parameter,
                        google::protobuf::compiler::GeneratorContext* context,
                        string* error) const {
    vector<pair<string, string> > options;
    google::protobuf::compiler::ParseGeneratorParameter(parameter, &options);

    bool generate_nano = false;
    for (int i = 0; i < options.size(); i++) {
      if (options[i].first == "nano" && options[i].second == "true") {
        generate_nano = true;
      }
    }

    string package_name = java_grpc_generator::ServiceJavaPackage(file);
    string package_filename = JavaPackageToDir(package_name);
    for (int i = 0; i < file->service_count(); ++i) {
      const google::protobuf::ServiceDescriptor* service = file->service(i);
      string filename = package_filename
          + java_grpc_generator::ServiceClassName(service) + ".java";
      std::unique_ptr<google::protobuf::io::ZeroCopyOutputStream> output(
          context->Open(filename));
      java_grpc_generator::GenerateService(service, output.get(), generate_nano);
    }
    return true;
  }
};

int main(int argc, char* argv[]) {
  JavaGrpcGenerator generator;
  return google::protobuf::compiler::PluginMain(argc, argv, &generator);
}
