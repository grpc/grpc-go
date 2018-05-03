/*
 * Copyright 2016 The gRPC Authors
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
import io.grpc.benchmarks.proto.Stats;
import io.grpc.benchmarks.qps.AsyncServer;
import io.grpc.internal.testing.TestUtils;
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
      BenchmarkServiceGrpc.getUnaryCallMethod().toBuilder(marshaller, marshaller)
          .build();

  /**
   * Generic version of the streaming ping-pong method call.
   */
  static final MethodDescriptor<ByteBuf, ByteBuf> GENERIC_STREAMING_PING_PONG_METHOD =
      BenchmarkServiceGrpc.getStreamingCallMethod().toBuilder(marshaller, marshaller)
          .build();

  private static final Logger log = Logger.getLogger(LoadServer.class.getName());

  private final Server server;
  private final AsyncServer.BenchmarkServiceImpl benchmarkService;
  private final OperatingSystemMXBean osBean;
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
    benchmarkService = new AsyncServer.BenchmarkServiceImpl();
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
    benchmarkService.shutdown();
    server.shutdownNow();
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
