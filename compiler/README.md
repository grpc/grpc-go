gRPC Java Plugin for Protobuf Compiler
==============================================

This generates the Java interfaces out of the service definition from a `.proto` file.

## System Requirement

* Linux
* The Github head of [Protobuf](https://github.com/google/protobuf) installed
* [Gradle](https://www.gradle.org/downloads) installed

## Compiling and Testing the Plugin
Change to the `compiler` directory:
```
$ cd $GRPC_JAVA_ROOT/compiler
```

To compile the plugin:
```
$ gradle java_pluginExecutable
```

To test the plugin with the compiler:
```
$ gradle test
```
You will see a `PASS` if the test succeeds.

To compile a proto file and generate Java interfaces out of the service definitions:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/java_plugin \
  --java_rpc_out="$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```
To generate Java interfaces with protobuf nano:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/java_plugin \
  --java_rpc_out=nano=true:"$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```
