gRPC Java Codegen Plugin for Protobuf Compiler
==============================================

This generates the Java interfaces out of the service definition from a
`.proto` file. It works with the Protobuf Compiler (``protoc``).

Normally you don't need to compile the codegen by yourself, since pre-compiled
binaries for common platforms are available on Maven Central. However, if the
pre-compiled binaries are not compatible with your system, you may want to
build your own codegen.

## System requirement

* Linux, Mac OS X with Clang, or Windows with MSYS2
* Java 7 or up
* [Protobuf](https://github.com/google/protobuf) 3.0.0-alpha-2 or up

## Compiling and testing the codegen
Change to the `compiler` directory:
```
$ cd $GRPC_JAVA_ROOT/compiler
```

To compile the plugin:
```
$ ../gradlew java_pluginExecutable
```

To test the plugin with the compiler:
```
$ ../gradlew test
```
You will see a `PASS` if the test succeeds.

To compile a proto file and generate Java interfaces out of the service definitions:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/protoc-gen-grpc-java \
  --java_rpc_out="$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```
To generate Java interfaces with protobuf nano:
```
$ protoc --plugin=protoc-gen-java_rpc=build/binaries/java_pluginExecutable/protoc-gen-grpc-java \
  --java_rpc_out=nano=true:"$OUTPUT_FILE" --proto_path="$DIR_OF_PROTO_FILE" "$PROTO_FILE"
```

## Installing the codegen to Maven local repository
This will compile a codegen and put it under your ``~/.m2/repository``. This
will make it available to any build tool that pulls codegens from Maven
repostiories.
```
$ ../gradlew install
```

## Pushing the codegen to Maven Central
Please follow the instructions in ``DEPLOYING.md`` under the root directory for
deploying the codegen to Maven Central.
