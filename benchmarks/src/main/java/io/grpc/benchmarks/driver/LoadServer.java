/*
 * Copyright 2016, Google Inc. All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 *    * Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *    * Redistributions in binary form must reproduce the above
 * copyright notice, this list of conditions and the following disclaimer
 * in the documentation and/or other materials provided with the
 * distribution.
 *
 *    * Neither the name of Google Inc. nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package io.grpc.benchmarks.driver;

import static java.util.concurrent.ForkJoinPool.defaultForkJoinWorkerThreadFactory;

import com.google.common.util.concurrent.UncaughtExceptionHandlers;
import com.sun.management.OperatingSystemMXBean;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.Marshaller;
import io.grpc.Server;
import io.grpc.ServerBuilder;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.benchmarks.ByteBufOutputMarshaller;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Messages;
import io.grpc.benchmarks.proto.Stats;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.TestUtils;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.PooledByteBufAllocator;
import java.io.File;
import java.lang.management.ManagementFactory;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.ForkJoinPool;
import java.util.concurrent.ForkJoinPool.ForkJoinWorkerThreadFactory;
import java.util.concurrent.ForkJoinWorkerThread;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Implements the server-side contract for the load testing scenarios.
 */
final class LoadServer {

  private static final Marshaller<ByteBuf> marshaller = new ByteBufOutputMarshaller();
  /**
   * Generic version of the unary method call.
   */
  static final MethodDescriptor<ByteBuf, ByteBuf> GENERIC_UNARY_METHOD =
      BenchmarkServiceGrpc.METHOD_UNARY_CALL.toBuilder(marshaller, marshaller)
          .build();

  /**
   * Generic version of the streaming ping-pong method call.
   */
  static final MethodDescriptor<ByteBuf, ByteBuf> GENERIC_STREAMING_PING_PONG_METHOD =
      BenchmarkServiceGrpc.METHOD_STREAMING_CALL.toBuilder(marshaller, marshaller)
          .build();

  private static final Logger log = Logger.getLogger(LoadServer.class.getName());

  private final Server server;
  private final BenchmarkServiceImpl benchmarkService;
  private final OperatingSystemMXBean osBean;
  private volatile boolean shutdown;
  private final int port;
  private ByteBuf genericResponse;
  private long lastStatTime;
  private long lastMarkCpuTime;

  LoadServer(Control.ServerConfig config) throws Exception {
    log.log(Level.INFO, "Server Config \n" + config.toString());
    port = config.getPort() ==  0 ? Utils.pickUnusedPort() : config.getPort();
    ServerBuilder<?> serverBuilder = ServerBuilder.forPort(port);
    int asyncThreads = config.getAsyncServerThreads() == 0
        ? Runtime.getRuntime().availableProcessors()
        : config.getAsyncServerThreads();
    // The concepts of sync & async server are quite different in the C impl and the names
    // chosen for the enum are based on that implementation. We use 'sync' to mean
    // the direct executor case in Java even though the service implementations are always
    // fully async.
    switch (config.getServerType()) {
      case ASYNC_SERVER: {
        serverBuilder.executor(getExecutor(asyncThreads));
        break;
      }
      case SYNC_SERVER: {
        serverBuilder.directExecutor();
        break;
      }
      case ASYNC_GENERIC_SERVER: {
        serverBuilder.executor(getExecutor(asyncThreads));
        // Create buffers for the generic service
        PooledByteBufAllocator alloc = PooledByteBufAllocator.DEFAULT;
        genericResponse = alloc.buffer(config.getPayloadConfig().getBytebufParams().getRespSize());
        if (genericResponse.capacity() > 0) {
          genericResponse.writerIndex(genericResponse.capacity() - 1);
        }
        break;
      }
      default: {
        throw new IllegalArgumentException();
      }
    }
    if (config.hasSecurityParams()) {
      File cert = TestUtils.loadCert("server1.pem");
      File key = TestUtils.loadCert("server1.key");
      serverBuilder.useTransportSecurity(cert, key);
    }
    benchmarkService = new BenchmarkServiceImpl();
    if (config.getServerType() == Control.ServerType.ASYNC_GENERIC_SERVER) {
      serverBuilder.addService(
          ServerServiceDefinition
              .builder(new ServiceDescriptor(BenchmarkServiceGrpc.SERVICE_NAME,
                  GENERIC_STREAMING_PING_PONG_METHOD))
              .addMethod(GENERIC_STREAMING_PING_PONG_METHOD, new GenericServiceCallHandler())
              .build());
    } else {
      serverBuilder.addService(benchmarkService);
    }
    server = serverBuilder.build();

    List<OperatingSystemMXBean> beans =
        ManagementFactory.getPlatformMXBeans(OperatingSystemMXBean.class);
    if (!beans.isEmpty()) {
      osBean = beans.get(0);
    } else {
      osBean = null;
    }
  }

  ExecutorService getExecutor(int asyncThreads) {
    // TODO(carl-mastrangelo): This should not be necessary.  I don't know where this should be
    // put.  Move it somewhere else, or remove it if no longer necessary.
    // See: https://github.com/grpc/grpc-java/issues/2119
    return new ForkJoinPool(asyncThreads,
        new ForkJoinWorkerThreadFactory() {
          final AtomicInteger num = new AtomicInteger();
          @Override
          public ForkJoinWorkerThread newThread(ForkJoinPool pool) {
            ForkJoinWorkerThread thread = defaultForkJoinWorkerThreadFactory.newThread(pool);
            thread.setDaemon(true);
            thread.setName("server-worker-" + "-" + num.getAndIncrement());
            return thread;
          }
        }, UncaughtExceptionHandlers.systemExit(), true /* async */);
  }

  int getPort() {
    return port;
  }

  int getCores() {
    return Runtime.getRuntime().availableProcessors();
  }

  void start() throws Exception {
    server.start();
    lastStatTime = System.nanoTime();
    if (osBean != null) {
      lastMarkCpuTime = osBean.getProcessCpuTime();
    }
  }

  Stats.ServerStats getStats() {
    Stats.ServerStats.Builder builder = Stats.ServerStats.newBuilder();
    long now = System.nanoTime();
    double elapsed = ((double) now - lastStatTime) / 1000000000.0;
    lastStatTime = now;
    builder.setTimeElapsed(elapsed);
    if (osBean != null) {
      // Report all the CPU time as user-time  (which is intentionally incorrect)
      long nowCpu = osBean.getProcessCpuTime();
      builder.setTimeUser(((double) nowCpu - lastMarkCpuTime) / 1000000000.0);
      lastMarkCpuTime = nowCpu;
    }
    return builder.build();
  }

  void shutdownNow() {
    shutdown = true;
    server.shutdownNow();
  }

  private class BenchmarkServiceImpl extends BenchmarkServiceGrpc.BenchmarkServiceImplBase {

    @Override
    public void unaryCall(Messages.SimpleRequest request,
                          StreamObserver<Messages.SimpleResponse> responseObserver) {
      responseObserver.onNext(Utils.makeResponse(request));
      responseObserver.onCompleted();
    }

    @Override
    public StreamObserver<Messages.SimpleRequest> streamingCall(
        final StreamObserver<Messages.SimpleResponse> responseObserver) {
      return new StreamObserver<Messages.SimpleRequest>() {
        @Override
        public void onNext(Messages.SimpleRequest value) {
          if (!shutdown) {
            responseObserver.onNext(Utils.makeResponse(value));
          } else {
            responseObserver.onCompleted();
          }
        }

        @Override
        public void onError(Throwable t) {
          responseObserver.onError(t);
        }

        @Override
        public void onCompleted() {
          responseObserver.onCompleted();
        }
      };
    }
  }

  private class GenericServiceCallHandler implements ServerCallHandler<ByteBuf, ByteBuf> {

    @Override
    public ServerCall.Listener<ByteBuf> startCall(
        final ServerCall<ByteBuf, ByteBuf> call, Metadata headers) {
      call.sendHeaders(new Metadata());
      call.request(1);
      return new ServerCall.Listener<ByteBuf>() {
        @Override
        public void onMessage(ByteBuf message) {
          // no-op
          message.release();
          call.request(1);
          call.sendMessage(genericResponse.slice());
        }

        @Override
        public void onHalfClose() {
          call.close(Status.OK, new Metadata());
        }

        @Override
        public void onCancel() {
        }

        @Override
        public void onComplete() {
        }
      };
    }
  }
}
