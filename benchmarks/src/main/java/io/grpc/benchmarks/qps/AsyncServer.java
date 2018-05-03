/*
 * Copyright 2015 The gRPC Authors
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
import com.google.protobuf.ByteString;
import io.grpc.Server;
import io.grpc.Status;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Messages;
import io.grpc.internal.testing.TestUtils;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.ServerCallStreamObserver;
import io.grpc.stub.StreamObserver;
import io.grpc.stub.StreamObservers;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.io.File;
import java.io.IOException;
import java.util.Iterator;
import java.util.concurrent.ForkJoinPool;
import java.util.concurrent.ForkJoinPool.ForkJoinWorkerThreadFactory;
import java.util.concurrent.ForkJoinWorkerThread;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Logger;

/**
 * QPS server using the non-blocking API.
 */
public class AsyncServer {
  private static final Logger log = Logger.getLogger(AsyncServer.class.getName());

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
        } catch (Exception e) {
          e.printStackTrace();
        }
      }
    });
    server.awaitTermination();
  }

  @SuppressWarnings("LiteralClassName") // Epoll is not available on windows
  static Server newServer(ServerConfiguration config) throws IOException {
    final EventLoopGroup boss;
    final EventLoopGroup worker;
    final Class<? extends ServerChannel> channelType;
    ThreadFactory tf = new DefaultThreadFactory("server-elg-", true /*daemon */);
    switch (config.transport) {
      case NETTY_NIO: {
        boss = new NioEventLoopGroup(1, tf);
        worker = new NioEventLoopGroup(0, tf);
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
          boss =
              (EventLoopGroup)
                  (groupClass
                      .getConstructor(int.class, ThreadFactory.class)
                      .newInstance(1, tf));
          worker =
              (EventLoopGroup)
                  (groupClass
                      .getConstructor(int.class, ThreadFactory.class)
                      .newInstance(0, tf));
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
          boss =
              (EventLoopGroup)
                  (groupClass
                      .getConstructor(int.class, ThreadFactory.class)
                      .newInstance(1, tf));
          worker =
              (EventLoopGroup)
                  (groupClass
                      .getConstructor(int.class, ThreadFactory.class)
                      .newInstance(0, tf));
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
        .flowControlWindow(config.flowControlWindow);
    if (config.tls) {
      System.out.println("Using fake CA for TLS certificate.\n"
          + "Run the Java client with --tls --testca");

      File cert = TestUtils.loadCert("server1.pem");
      File key = TestUtils.loadCert("server1.key");
      builder.useTransportSecurity(cert, key);
    }
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
    // Always use the same canned response for bidi. This is allowed by the spec.
    private static final int BIDI_RESPONSE_BYTES = 100;
    private static final Messages.SimpleResponse BIDI_RESPONSE = Messages.SimpleResponse
        .newBuilder()
        .setPayload(Messages.Payload.newBuilder()
            .setBody(ByteString.copyFrom(new byte[BIDI_RESPONSE_BYTES])).build())
        .build();

    private final AtomicBoolean shutdown = new AtomicBoolean();

    public BenchmarkServiceImpl() {
    }

    public void shutdown() {
      shutdown.set(true);
    }

    @Override
    public void unaryCall(Messages.SimpleRequest request,
        StreamObserver<Messages.SimpleResponse> responseObserver) {
      responseObserver.onNext(Utils.makeResponse(request));
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<Messages.SimpleRequest> streamingCall(
        final StreamObserver<Messages.SimpleResponse> observer) {
      final ServerCallStreamObserver<Messages.SimpleResponse> responseObserver =
          (ServerCallStreamObserver<Messages.SimpleResponse>) observer;
      // TODO(spencerfang): flow control to stop reading when !responseObserver.isReady
      return new StreamObserver<Messages.SimpleRequest>() {
        @Override
        public void onNext(Messages.SimpleRequest value) {
          if (shutdown.get()) {
            responseObserver.onCompleted();
            return;
          }
          responseObserver.onNext(Utils.makeResponse(value));
        }

        @Override
        public void onError(Throwable t) {
          // other side closed with non OK
          responseObserver.onError(t);
        }

        @Override
        public void onCompleted() {
          // other side closed with OK
          responseObserver.onCompleted();
        }
      };
    }

    @Override
    public StreamObserver<Messages.SimpleRequest> streamingFromClient(
        final StreamObserver<Messages.SimpleResponse> responseObserver) {
      return new StreamObserver<Messages.SimpleRequest>() {
        Messages.SimpleRequest lastSeen = null;

        @Override
        public void onNext(Messages.SimpleRequest value) {
          if (shutdown.get()) {
            responseObserver.onCompleted();
            return;
          }
          lastSeen = value;
        }

        @Override
        public void onError(Throwable t) {
          // other side closed with non OK
          responseObserver.onError(t);
        }

        @Override
        public void onCompleted() {
          if (lastSeen != null) {
            responseObserver.onNext(Utils.makeResponse(lastSeen));
            responseObserver.onCompleted();
          } else {
            responseObserver.onError(
                Status.FAILED_PRECONDITION
                    .withDescription("never received any requests").asException());
          }
        }
      };
    }

    @Override
    public void streamingFromServer(
        final Messages.SimpleRequest request,
        final StreamObserver<Messages.SimpleResponse> observer) {
      // send forever, until the client cancels or we shut down
      final Messages.SimpleResponse response = Utils.makeResponse(request);
      final ServerCallStreamObserver<Messages.SimpleResponse> responseObserver =
          (ServerCallStreamObserver<Messages.SimpleResponse>) observer;
      // If the client cancels, copyWithFlowControl takes care of calling
      // responseObserver.onCompleted() for us
      StreamObservers.copyWithFlowControl(
          new Iterator<Messages.SimpleResponse>() {
            @Override
            public boolean hasNext() {
              return !shutdown.get() && !responseObserver.isCancelled();
            }

            @Override
            public Messages.SimpleResponse next() {
              return response;
            }

            @Override
            public void remove() {
              throw new UnsupportedOperationException();
            }
          },
          responseObserver);
    }

    @Override
    public StreamObserver<Messages.SimpleRequest> streamingBothWays(
        final StreamObserver<Messages.SimpleResponse> observer) {
      // receive data forever and send data forever until client cancels or we shut down.
      final ServerCallStreamObserver<Messages.SimpleResponse> responseObserver =
          (ServerCallStreamObserver<Messages.SimpleResponse>) observer;
      // If the client cancels, copyWithFlowControl takes care of calling
      // responseObserver.onCompleted() for us
      StreamObservers.copyWithFlowControl(
          new Iterator<Messages.SimpleResponse>() {
            @Override
            public boolean hasNext() {
              return !shutdown.get() && !responseObserver.isCancelled();
            }

            @Override
            public Messages.SimpleResponse next() {
              return BIDI_RESPONSE;
            }

            @Override
            public void remove() {
              throw new UnsupportedOperationException();
            }
          },
          responseObserver
      );

      return new StreamObserver<Messages.SimpleRequest>() {
        @Override
        public void onNext(final Messages.SimpleRequest request) {
          // noop
        }

        @Override
        public void onError(Throwable t) {
          // other side cancelled
        }

        @Override
        public void onCompleted() {
          // Should never happen, because clients should cancel this call in order to stop
          // the operation. Also because copyWithFlowControl hogs the inbound network thread
          // via the handler for onReady, we would never expect this callback to be able to
          // run anyways.
          log.severe("clients should CANCEL the call to stop bidi streaming");
        }
      };
    }
  }
}
