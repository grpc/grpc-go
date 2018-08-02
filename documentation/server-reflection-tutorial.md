# gRPC Server Reflection Tutorial

gRPC Server Reflection provides information about publicly-accessible gRPC
services on a server, and assists clients at runtime with constructing RPC
requests and responses without precompiled service information. It is used by
the gRPC command line tool (gRPC CLI), which can be used to introspect server
protos and send/receive test RPCs. Reflection is only supported for
proto-based services.

## Enable Server Reflection

gRPC-Java Server Reflection is implemented by
`io.grpc.protobuf.services.ProtoReflectionService` in the `grpc-services`
package. To enable server reflection, you need to add the
`ProtoReflectionService` to your gRPC server.

For example, to enable server reflection in
`examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java`, we
need to make the following changes:

```diff
--- a/examples/build.gradle
+++ b/examples/build.gradle
@@ -27,6 +27,7 @@
 dependencies {
   compile "io.grpc:grpc-netty-shaded:${grpcVersion}"
   compile "io.grpc:grpc-protobuf:${grpcVersion}"
+  compile "io.grpc:grpc-services:${grpcVersion}"
   compile "io.grpc:grpc-stub:${grpcVersion}"
 
   testCompile "junit:junit:4.12"
--- a/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
+++ b/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
@@ -33,6 +33,7 @@ package io.grpc.examples.helloworld;
 
 import io.grpc.Server;
 import io.grpc.ServerBuilder;
+import io.grpc.protobuf.services.ProtoReflectionService;
 import io.grpc.stub.StreamObserver;
 import java.io.IOException;
 import java.util.logging.Logger;
@@ -50,6 +51,7 @@ public class HelloWorldServer {
     int port = 50051;
     server = ServerBuilder.forPort(port)
         .addService(new GreeterImpl())
+        .addService(ProtoReflectionService.newInstance())
         .build()
         .start();
     logger.info("Server started, listening on " + port);
```

In the following examples, we assume you have made these changes to
enable reflection in `HelloWorldServer.java`.

## gRPC CLI

After enabling server reflection in a server application, you can use gRPC
CLI to get information about its available services. gRPC CLI is written
in C++. Instructions on how to build gRPC CLI can be found at
[command_line_tool.md](https://github.com/grpc/grpc/blob/master/doc/command_line_tool.md).

## Use gRPC CLI to check services

First, build and start the `hello-world-server`:

```sh
$ cd examples/
$ ./gradlew installDist
$ build/install/examples/bin/hello-world-server
```

Open a new terminal and make sure you are in the directory where the `grpc_cli`
binary is located:

```sh
$ cd <grpc-cpp-directory>/bins/opt
```

### List services

`grpc_cli ls` command lists services and methods exposed at a given port:

- List all the services exposed at a given port

  ```sh
  $ ./grpc_cli ls localhost:50051
  ```

  output:
  ```sh
  helloworld.Greeter
  grpc.reflection.v1alpha.ServerReflection
  ```

- List one service with details

  `grpc_cli ls` command inspects a service given its full name (in the format of
  \<package\>.\<service\>). It can print information with a long listing format
  when `-l` flag is set. This flag can be used to get more details about a
  service.

  ```sh
  $ ./grpc_cli ls localhost:50051 helloworld.Greeter -l
  ```

  output:
  ```sh
  filename: helloworld.proto
  package: helloworld;
  service Greeter {
    rpc SayHello(helloworld.HelloRequest) returns (helloworld.HelloReply) {}
  }

  ```

### List methods

- List one method with details

  `grpc_cli ls` command also inspects a method given its full name (in the
  format of \<package\>.\<service\>.\<method\>).

  ```sh
  $ ./grpc_cli ls localhost:50051 helloworld.Greeter.SayHello -l
  ```

  output:
  ```sh
    rpc SayHello(helloworld.HelloRequest) returns (helloworld.HelloReply) {}
  ```

### Inspect message types

We can use`grpc_cli type` command to inspect request/response types given the
full name of the type (in the format of \<package\>.\<type\>).

- Get information about the request type

  ```sh
  $ ./grpc_cli type localhost:50051 helloworld.HelloRequest
  ```

  output:
  ```sh
  message HelloRequest {
    optional string name = 1[json_name = "name"];
  }
  ```

### Call a remote method

We can send RPCs to a server and get responses using `grpc_cli call` command.

- Call a unary method

  ```sh
  $ ./grpc_cli call localhost:50051 SayHello "name: 'gRPC CLI'"
  ```

  output:
  ```sh
  message: "Hello gRPC CLI"
  ```
