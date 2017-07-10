/*
 * Copyright 2015, gRPC Authors All rights reserved.
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
 */

package io.grpc.benchmarks.qps;

import com.google.common.util.concurrent.UncaughtExceptionHandlers;
import io.grpc.Server;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Messages.SimpleRequest;
import io.grpc.benchmarks.proto.Messages.SimpleResponse;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.GrpcSslContexts;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.StreamObserver;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.handler.ssl.SslContext;
import io.netty.handler.ssl.SslContextBuilder;
import io.netty.handler.ssl.SslProvider;
import java.io.File;
import java.io.IOException;
import java.util.concurrent.ForkJoinPool;
import java.util.concurrent.ForkJoinPool.ForkJoinWorkerThreadFactory;
import java.util.concurrent.ForkJoinWorkerThread;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/**
 * QPS server using the non-blocking API.
 */
public class AsyncServer {

  /**
   * checkstyle complains if there is no javadoc comment here.
   */
  public static void main(String... args) throws Exception {
    new AsyncServer().run(args);
  }

  /** Equivalent of "main", but non-static. */
  public void run(String[] args) throws Exception {
    ServerConfiguration.Builder configBuilder = ServerConfiguration.newBuilder();
    ServerConfiguration config;
    try {
      config = configBuilder.build(args);
    } catch (Exception e) {
      System.out.println(e.getMessage());
      configBuilder.printUsage();
      return;
    }

    final Server server = newServer(config);
    server.start();

    System.out.println("QPS Server started on " + config.address);

    Runtime.getRuntime().addShutdownHook(new Thread() {
      @Override
      public void run() {
        try {
          System.out.println("QPS Server shutting down");
          server.shutdown();
          server.awaitTermination(5, TimeUnit.SECONDS);
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });
  }

  @SuppressWarnings("LiteralClassName") // Epoll is not available on windows
  static Server newServer(ServerConfiguration config) throws IOException {
    SslContext sslContext = null;
    if (config.tls) {
      System.out.println("Using fake CA for TLS certificate.\n"
          + "Run the Java client with --tls --testca");

      File cert = TestUtils.loadCert("server1.pem");
      File key = TestUtils.loadCert("server1.key");
      SslContextBuilder sslContextBuilder = GrpcSslContexts.forServer(cert, key);
      if (config.transport == ServerConfiguration.Transport.NETTY_NIO) {
        sslContextBuilder = GrpcSslContexts.configure(sslContextBuilder, SslProvider.JDK);
      } else {
        // Native transport with OpenSSL
        sslContextBuilder = GrpcSslContexts.configure(sslContextBuilder, SslProvider.OPENSSL);
      }
      if (config.useDefaultCiphers) {
        sslContextBuilder.ciphers(null);
      }
      sslContext = sslContextBuilder.build();
    }

    final EventLoopGroup boss;
    final EventLoopGroup worker;
    final Class<? extends ServerChannel> channelType;
    switch (config.transport) {
      case NETTY_NIO: {
        boss = new NioEventLoopGroup();
        worker = new NioEventLoopGroup();
        channelType = NioServerSocketChannel.class;
        break;
      }
      case NETTY_EPOLL: {
        try {
          // These classes are only available on linux.
          Class<?> groupClass = Class.forName("io.netty.channel.epoll.EpollEventLoopGroup");
          @SuppressWarnings("unchecked")
          Class<? extends ServerChannel> channelClass = (Class<? extends ServerChannel>)
              Class.forName("io.netty.channel.epoll.EpollServerSocketChannel");
          boss = (EventLoopGroup) groupClass.getConstructor().newInstance();
          worker = (EventLoopGroup) groupClass.getConstructor().newInstance();
          channelType = channelClass;
          break;
        } catch (Exception e) {
          throw new RuntimeException(e);
        }
      }
      case NETTY_UNIX_DOMAIN_SOCKET: {
        try {
          // These classes are only available on linux.
          Class<?> groupClass = Class.forName("io.netty.channel.epoll.EpollEventLoopGroup");
          @SuppressWarnings("unchecked")
          Class<? extends ServerChannel> channelClass = (Class<? extends ServerChannel>)
              Class.forName("io.netty.channel.epoll.EpollServerDomainSocketChannel");
          boss = (EventLoopGroup) groupClass.getConstructor().newInstance();
          worker = (EventLoopGroup) groupClass.getConstructor().newInstance();
          channelType = channelClass;
          break;
        } catch (Exception e) {
          throw new RuntimeException(e);
        }
      }
      default: {
        // Should never get here.
        throw new IllegalArgumentException("Unsupported transport: " + config.transport);
      }
    }

    NettyServerBuilder builder = NettyServerBuilder
        .forAddress(config.address)
        .bossEventLoopGroup(boss)
        .workerEventLoopGroup(worker)
        .channelType(channelType)
        .addService(new BenchmarkServiceImpl())
        .sslContext(sslContext)
        .flowControlWindow(config.flowControlWindow);
    if (config.directExecutor) {
      builder.directExecutor();
    } else {
      // TODO(carl-mastrangelo): This should not be necessary.  I don't know where this should be
      // put.  Move it somewhere else, or remove it if no longer necessary.
      // See: https://github.com/grpc/grpc-java/issues/2119
      builder.executor(new ForkJoinPool(Runtime.getRuntime().availableProcessors(),
          new ForkJoinWorkerThreadFactory() {
            final AtomicInteger num = new AtomicInteger();
            @Override
            public ForkJoinWorkerThread newThread(ForkJoinPool pool) {
              ForkJoinWorkerThread thread =
                  ForkJoinPool.defaultForkJoinWorkerThreadFactory.newThread(pool);
              thread.setDaemon(true);
              thread.setName("grpc-server-app-" + "-" + num.getAndIncrement());
              return thread;
            }
          }, UncaughtExceptionHandlers.systemExit(), true /* async */));
    }

    return builder.build();
  }

  public static class BenchmarkServiceImpl extends BenchmarkServiceGrpc.BenchmarkServiceImplBase {

    @Override
    public void unaryCall(SimpleRequest request, StreamObserver<SimpleResponse> responseObserver) {
      SimpleResponse response = Utils.makeResponse(request);
      responseObserver.onNext(response);
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<SimpleRequest> streamingCall(
        final StreamObserver<SimpleResponse> responseObserver) {
      return new StreamObserver<SimpleRequest>() {
        @Override
        public void onNext(SimpleRequest request) {
          SimpleResponse response = Utils.makeResponse(request);
          responseObserver.onNext(response);
        }

        @Override
        public void onError(Throwable t) {
          System.out.println("Encountered an error in streamingCall");
          t.printStackTrace();
        }

        @Override
        public void onCompleted() {
          responseObserver.onCompleted();
        }
      };
    }

  }
}
