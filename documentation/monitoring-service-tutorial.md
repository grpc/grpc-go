# gRPC Monitoring Service Tutorial

***Note:*** *The monitoring service requires the `instrumentation-java` library
implementations, which are still being developed. The steps in this tutorial
will not work until the `instrumentation-java` implementation is released.*

The gRPC monitoring service provides access to metrics such as RPC latencies,
bytes sent and received, and error counts. By default, the monitoring service
will expose metrics recorded by all gRPC clients and servers in the same
application.

# Requirement: instrumentation-java

For the monitoring service to start, gRPC must have access to the
`instrumentation-java` library. By default, gRPC depends only on the
`instrumentation` APIs. The actual implementations have not yet been released.

**TODO(ericgribkoff):** Add instructions for including instrumentation in
`build.gradle` once `instrumentation-java` is released.

# Adding the service to a server

The gRPC monitoring service is implemented by
`io.grpc.services.MonitoringService` in the `grpc-services` package. The service
itself is defined in
[`grpc/instrumentation/v1alpha/monitoring.proto`](https://github.com/grpc/grpc-proto/blob/master/grpc/instrumentation/v1alpha/monitoring.proto).
Currently only the `GetCanonicalRpcStats` method is supported.

To run the monitoring service, it must be added to a server. While the
monitoring service can be added to any existing server, keep in mind that the
service will be exposing potentially sensitive information about your
applications. It is up to you to make sure that the server hosting the
monitoring service is secure. Typically, this will mean running a separate gRPC
server to host the monitoring service.

To start the `HelloWorldServer` with a second server on port `50052` for the
monitoring service, we need to make the following changes:

```diff
--- a/examples/build.gradle
+++ b/examples/build.gradle
@@ -27,6 +27,7 @@
 dependencies {
   compile "io.grpc:grpc-netty:${grpcVersion}"
   compile "io.grpc:grpc-protobuf:${grpcVersion}"
+  compile "io.grpc:grpc-services:${grpcVersion}"
   compile "io.grpc:grpc-stub:${grpcVersion}"
 
   testCompile "junit:junit:4.11"
--- a/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
+++ b/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldServer.java
@@ -33,6 +33,8 @@ package io.grpc.examples.helloworld;
 
 import io.grpc.Server;
 import io.grpc.ServerBuilder;
+import io.grpc.protobuf.services.ProtoReflectionService;
+import io.grpc.services.MonitoringService;
 import io.grpc.stub.StreamObserver;
 import java.io.IOException;
 import java.util.logging.Logger;
@@ -44,6 +46,7 @@ public class HelloWorldServer {
   private static final Logger logger = Logger.getLogger(HelloWorldServer.class.getName());
 
   private Server server;
+  private Server monitoringServer;
 
   private void start() throws IOException {
     /* The port on which the server should run */
@@ -53,6 +56,14 @@ public class HelloWorldServer {
         .build()
         .start();
     logger.info("Server started, listening on " + port);
+    int monitoringPort = 50052;
+    /* This will throw an exception if StatsManager from instrumentation-java is unavailable */
+    monitoringServer = ServerBuilder.forPort(monitoringPort)
+        .addService(MonitoringService.getInstance())
+        .addService(ProtoReflectionService.newInstance())
+        .build()
+        .start();
+    logger.info("Monitoring server started, listening on " + monitoringPort);
     Runtime.getRuntime().addShutdownHook(new Thread() {
       @Override
       public void run() {
@@ -68,6 +79,9 @@ public class HelloWorldServer {
     if (server != null) {
       server.shutdown();
     }
+    if (monitoringServer != null) {
+      monitoringServer.shutdown();
+    }
   }
 
   /**
```

Note that the inclusion of the `ProtoReflectionService` is optional, but it will
allow us to easily query the monitoring service using the `grpc_cli` tool (see
the next section).

The next example demonstrates running a server with the monitoring service on a
Unix domain socket (`/tmp/grpc_monitoring.sock`) alongside the
`HelloWorldClient`. This will only work on `linux_x86_64` architectures.

```diff
--- a/examples/build.gradle
+++ b/examples/build.gradle
@@ -27,7 +27,10 @@
 dependencies {
   compile "io.grpc:grpc-netty:${grpcVersion}"
   compile "io.grpc:grpc-protobuf:${grpcVersion}"
+  compile "io.grpc:grpc-services:${grpcVersion}"
   compile "io.grpc:grpc-stub:${grpcVersion}"
+  // This must match the version of netty used by grpcVersion
+  compile "io.netty:netty-transport-native-epoll:4.1.8.Final:linux-x86_64"
 
   testCompile "junit:junit:4.11"
   testCompile "org.mockito:mockito-core:1.9.5"
--- a/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldClient.java
+++ b/examples/src/main/java/io/grpc/examples/helloworld/HelloWorldClient.java
@@ -33,7 +33,18 @@ package io.grpc.examples.helloworld;
 
 import io.grpc.ManagedChannel;
 import io.grpc.ManagedChannelBuilder;
+import io.grpc.Server;
 import io.grpc.StatusRuntimeException;
+import io.grpc.netty.NettyServerBuilder;
+import io.grpc.protobuf.services.ProtoReflectionService;
+import io.grpc.services.MonitoringService;
+import io.netty.channel.EventLoopGroup;
+import io.netty.channel.epoll.EpollEventLoopGroup;
+import io.netty.channel.epoll.EpollServerDomainSocketChannel;
+import io.netty.channel.unix.DomainSocketAddress;
+import java.io.IOException;
+import java.nio.file.FileSystems;
+import java.nio.file.Files;
 import java.util.concurrent.TimeUnit;
 import java.util.logging.Level;
 import java.util.logging.Logger;
@@ -79,11 +90,50 @@ public class HelloWorldClient {
     logger.info("Greeting: " + response.getMessage());
   }
 
+  private static class MonitoringServer {
+    private Server server;
+    private EventLoopGroup bossEventLoopGroup;
+    private EventLoopGroup workerEventLoopGroup;
+
+    private void start(String udsFilename) throws IOException {
+      logger.info("Trying to start monitoring service on " + udsFilename);
+      logger.info("Will first delete " + udsFilename + " if it exists");
+      Files.deleteIfExists(FileSystems.getDefault().getPath(udsFilename));
+      bossEventLoopGroup = new EpollEventLoopGroup();
+      workerEventLoopGroup = new EpollEventLoopGroup();
+      /* This will throw an exception if StatsManager from instrumentation-java is unavailable */
+      server =
+          NettyServerBuilder.forAddress(new DomainSocketAddress(udsFilename))
+              .channelType(EpollServerDomainSocketChannel.class)
+              .bossEventLoopGroup(bossEventLoopGroup)
+              .workerEventLoopGroup(workerEventLoopGroup)
+              .addService(MonitoringService.getInstance())
+              .addService(ProtoReflectionService.newInstance())
+              .build();
+      server.start();
+      logger.info("Monitoring service started on " + udsFilename);
+      Runtime.getRuntime().addShutdownHook(new Thread() {
+        public void run() {
+          MonitoringServer.this.stop();
+        }
+      });
+    }
+
+    private void stop() {
+      server.shutdown();
+      bossEventLoopGroup.shutdownGracefully(0, 0, TimeUnit.SECONDS);
+      workerEventLoopGroup.shutdownGracefully(0, 0, TimeUnit.SECONDS);
+    }
+  }
+
   /**
    * Greet server. If provided, the first element of {@code args} is the name to use in the
    * greeting.
    */
   public static void main(String[] args) throws Exception {
+    String udsFilename = "/tmp/grpc_monitoring.sock";
+    MonitoringServer monitoringServer = new MonitoringServer();
+    monitoringServer.start(udsFilename);
     HelloWorldClient client = new HelloWorldClient("localhost", 50051);
     try {
       /* Access a service running on the local machine on port 50051 */
@@ -92,8 +142,11 @@ public class HelloWorldClient {
         user = args[0]; /* Use the arg as the name to greet if provided */
       }
       client.greet(user);
+      /* Sleep for 60 seconds to allow time for querying the monitoring server */
+      TimeUnit.SECONDS.sleep(60);
     } finally {
       client.shutdown();
+      monitoringServer.stop();
     }
   }
 }
```

# Querying the service

The monitoring service's `GetCanonicalStats` method returns a list of stats
recorded by gRPC, including latencies, bytes sent and received, and error
counts.

You can query the service via the
[`grpc_cli`](https://github.com/grpc/grpc/blob/master/doc/command_line_tool.md)
command line tool as follows:

```sh
$ bins/opt/grpc_cli call localhost:50052 \
        grpc.instrumentation.v1alpha.Monitoring.GetCanonicalRpcStats ''
```

If the server is running on a UDS, it can be queried as follows:

```sh
$ bins/opt/grpc_cli call 'unix:///tmp/grpc_monitoring.sock' \
        grpc.instrumentation.v1alpha.Monitoring.GetCanonicalRpcStats ''
```

The response data comes as
[`instrumentation`](https://github.com/google/instrumentation-proto/blob/master/stats/census.proto)
protocol buffers. For each metric, such as `rpc_client_errors`, there is a
`google.instrumentation.MeasurementDescriptor` and
`google.instrumentation.ViewDescriptor` which describe the measurement and view,
respectively, and a `google.instrumentation.View` that contains the actual data.
The comments on the [proto
file](https://github.com/google/instrumentation-proto/blob/master/stats/census.proto)
describe the contents of each of these types.
