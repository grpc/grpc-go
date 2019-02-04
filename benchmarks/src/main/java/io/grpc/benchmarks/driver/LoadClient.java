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

import com.sun.management.OperatingSystemMXBean;
import io.grpc.CallOptions;
import io.grpc.Channel;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.Status;
import io.grpc.benchmarks.Transport;
import io.grpc.benchmarks.Utils;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Control;
import io.grpc.benchmarks.proto.Messages;
import io.grpc.benchmarks.proto.Payloads;
import io.grpc.benchmarks.proto.Stats;
import io.grpc.stub.ClientCalls;
import io.grpc.stub.StreamObserver;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.PooledByteBufAllocator;
import io.netty.channel.epoll.Epoll;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.lang.management.ManagementFactory;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Semaphore;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.concurrent.locks.LockSupport;
import java.util.logging.Level;
import java.util.logging.Logger;
import org.HdrHistogram.Histogram;
import org.HdrHistogram.LogarithmicIterator;
import org.HdrHistogram.Recorder;
import org.apache.commons.math3.distribution.ExponentialDistribution;

/**
 * Implements the client-side contract for the load testing scenarios.
 */
class LoadClient {

  private static final Logger log = Logger.getLogger(LoadClient.class.getName());
  private ByteBuf genericRequest;

  private final Control.ClientConfig config;
  private final ExponentialDistribution distribution;
  private volatile boolean shutdown;
  private final int threadCount;

  ManagedChannel[] channels;
  BenchmarkServiceGrpc.BenchmarkServiceBlockingStub[] blockingStubs;
  BenchmarkServiceGrpc.BenchmarkServiceStub[] asyncStubs;
  Recorder recorder;
  private ExecutorService fixedThreadPool;
  private Messages.SimpleRequest simpleRequest;
  private final OperatingSystemMXBean osBean;
  private long lastMarkCpuTime;

  LoadClient(Control.ClientConfig config) throws Exception {
    log.log(Level.INFO, "Client Config \n" + config.toString());
    this.config = config;
    // Create the channels
    channels = new ManagedChannel[config.getClientChannels()];
    for (int i = 0; i < config.getClientChannels(); i++) {
      channels[i] =
          Utils.newClientChannel(
              Epoll.isAvailable() ?  Transport.NETTY_EPOLL : Transport.NETTY_NIO,
              Utils.parseSocketAddress(config.getServerTargets(i % config.getServerTargetsCount())),
              config.hasSecurityParams(),
              config.hasSecurityParams() && config.getSecurityParams().getUseTestCa(),
              config.hasSecurityParams()
                  ? config.getSecurityParams().getServerHostOverride()
                  : null,
              Utils.DEFAULT_FLOW_CONTROL_WINDOW,
              false);
    }

    // Create a stub per channel
    if (config.getClientType() == Control.ClientType.ASYNC_CLIENT) {
      asyncStubs = new BenchmarkServiceGrpc.BenchmarkServiceStub[channels.length];
      for (int i = 0; i < channels.length; i++) {
        asyncStubs[i] = BenchmarkServiceGrpc.newStub(channels[i]);
      }
    } else {
      blockingStubs = new BenchmarkServiceGrpc.BenchmarkServiceBlockingStub[channels.length];
      for (int i = 0; i < channels.length; i++) {
        blockingStubs[i] = BenchmarkServiceGrpc.newBlockingStub(channels[i]);
      }
    }

    // Determine no of threads
    if (config.getClientType() == Control.ClientType.SYNC_CLIENT) {
      threadCount = config.getOutstandingRpcsPerChannel() * config.getClientChannels();
    } else {
      threadCount = config.getAsyncClientThreads() == 0
          ? Runtime.getRuntime().availableProcessors()
          : config.getAsyncClientThreads();
    }
    // Use a fixed sized pool of daemon threads.
    fixedThreadPool = Executors.newFixedThreadPool(threadCount,
        new DefaultThreadFactory("client-worker", true));

    // Create the load distribution
    switch (config.getLoadParams().getLoadCase()) {
      case CLOSED_LOOP:
        distribution = null;
        break;
      case LOAD_NOT_SET:
        distribution = null;
        break;
      case POISSON:
        // Mean of exp distribution per thread is <no threads> / <offered load per second>
        distribution = new ExponentialDistribution(
            threadCount / config.getLoadParams().getPoisson().getOfferedLoad());
        break;
      default:
        throw new IllegalArgumentException("Scenario not implemented");
    }

    // Create payloads
    switch (config.getPayloadConfig().getPayloadCase()) {
      case SIMPLE_PARAMS: {
        Payloads.SimpleProtoParams simpleParams = config.getPayloadConfig().getSimpleParams();
        simpleRequest = Utils.makeRequest(Messages.PayloadType.COMPRESSABLE,
            simpleParams.getReqSize(), simpleParams.getRespSize());
        break;
      }
      case BYTEBUF_PARAMS: {
        PooledByteBufAllocator alloc = PooledByteBufAllocator.DEFAULT;
        genericRequest = alloc.buffer(config.getPayloadConfig().getBytebufParams().getRespSize());
        if (genericRequest.capacity() > 0) {
          genericRequest.writerIndex(genericRequest.capacity() - 1);
        }
        break;
      }
      default: {
        // Not implemented yet
        throw new IllegalArgumentException("Scenario not implemented");
      }
    }

    List<OperatingSystemMXBean> beans =
        ManagementFactory.getPlatformMXBeans(OperatingSystemMXBean.class);
    if (!beans.isEmpty()) {
      osBean = beans.get(0);
    } else {
      osBean = null;
    }

    // Create the histogram recorder
    recorder = new Recorder((long) config.getHistogramParams().getMaxPossible(), 3);
  }

  /**
   * Start the load scenario.
   */
  void start() {
    Runnable r;
    for (int i = 0; i < threadCount; i++) {
      r = null;
      switch (config.getPayloadConfig().getPayloadCase()) {
        case SIMPLE_PARAMS: {
          if (config.getClientType() == Control.ClientType.SYNC_CLIENT) {
            if (config.getRpcType() == Control.RpcType.UNARY) {
              r = new BlockingUnaryWorker(blockingStubs[i % blockingStubs.length]);
            }
          } else if (config.getClientType() == Control.ClientType.ASYNC_CLIENT) {
            if (config.getRpcType() == Control.RpcType.UNARY) {
              r = new AsyncUnaryWorker(asyncStubs[i % asyncStubs.length]);
            } else if (config.getRpcType() == Control.RpcType.STREAMING) {
              r = new AsyncPingPongWorker(asyncStubs[i % asyncStubs.length]);
            }
          }
          break;
        }
        case BYTEBUF_PARAMS: {
          if (config.getClientType() == Control.ClientType.SYNC_CLIENT) {
            if (config.getRpcType() == Control.RpcType.UNARY) {
              r = new GenericBlockingUnaryWorker(channels[i % channels.length]);
            }
          } else if (config.getClientType() == Control.ClientType.ASYNC_CLIENT) {
            if (config.getRpcType() == Control.RpcType.UNARY) {
              r = new GenericAsyncUnaryWorker(channels[i % channels.length]);
            } else if (config.getRpcType() == Control.RpcType.STREAMING) {
              r = new GenericAsyncPingPongWorker(channels[i % channels.length]);
            }
          }

          break;
        }
        default: {
          throw Status.UNIMPLEMENTED.withDescription(
              "Unknown payload case " + config.getPayloadConfig().getPayloadCase().name())
              .asRuntimeException();
        }
      }
      if (r == null) {
        throw new IllegalStateException(config.getRpcType().name()
            + " not supported for client type "
            + config.getClientType());
      }
      fixedThreadPool.execute(r);
    }
    if (osBean != null) {
      lastMarkCpuTime = osBean.getProcessCpuTime();
    }
  }

  /**
   * Take a snapshot of the statistics which can be returned to the driver.
   */
  Stats.ClientStats getStats() {
    Histogram intervalHistogram = recorder.getIntervalHistogram();

    Stats.ClientStats.Builder statsBuilder = Stats.ClientStats.newBuilder();
    Stats.HistogramData.Builder latenciesBuilder = statsBuilder.getLatenciesBuilder();
    double resolution = 1.0 + Math.max(config.getHistogramParams().getResolution(), 0.01);
    LogarithmicIterator logIterator = new LogarithmicIterator(intervalHistogram, 1,
        resolution);
    double base = 1;
    while (logIterator.hasNext()) {
      latenciesBuilder.addBucket((int) logIterator.next().getCountAddedInThisIterationStep());
      base = base * resolution;
    }
    // Driver expects values for all buckets in the range, not just the range of buckets that
    // have values.
    while (base < config.getHistogramParams().getMaxPossible()) {
      latenciesBuilder.addBucket(0);
      base = base * resolution;
    }
    latenciesBuilder.setMaxSeen(intervalHistogram.getMaxValue());
    latenciesBuilder.setMinSeen(intervalHistogram.getMinNonZeroValue());
    latenciesBuilder.setCount(intervalHistogram.getTotalCount());
    latenciesBuilder.setSum(intervalHistogram.getMean()
        * intervalHistogram.getTotalCount());
    // TODO: No support for sum of squares

    statsBuilder.setTimeElapsed((intervalHistogram.getEndTimeStamp()
        - intervalHistogram.getStartTimeStamp()) / 1000.0);
    if (osBean != null) {
      // Report all the CPU time as user-time  (which is intentionally incorrect)
      long nowCpu = osBean.getProcessCpuTime();
      statsBuilder.setTimeUser(((double) nowCpu - lastMarkCpuTime) / 1000000000.0);
      lastMarkCpuTime = nowCpu;
    }
    return statsBuilder.build();
  }

  /**
   * Shutdown the scenario as cleanly as possible.
   */
  void shutdownNow() {
    shutdown = true;
    for (int i = 0; i < channels.length; i++) {
      // Initiate channel shutdown
      channels[i].shutdown();
    }
    for (int i = 0; i < channels.length; i++) {
      try {
        // Wait for channel termination
        channels[i].awaitTermination(1, TimeUnit.SECONDS);
      } catch (InterruptedException ie) {
        channels[i].shutdownNow();
      }
    }
    fixedThreadPool.shutdownNow();
  }

  /**
   * Record the event elapsed time to the histogram and delay initiation of the next event based
   * on the load distribution.
   */
  void delay(long alreadyElapsed) {
    recorder.recordValue(alreadyElapsed);
    if (distribution != null) {
      long nextPermitted = Math.round(distribution.sample() * 1000000000.0);
      if (nextPermitted > alreadyElapsed) {
        LockSupport.parkNanos(nextPermitted - alreadyElapsed);
      }
    }
  }

  /**
   * Worker which executes blocking unary calls. Event timing is the duration between sending the
   * request and receiving the response.
   */
  class BlockingUnaryWorker implements Runnable {
    final BenchmarkServiceGrpc.BenchmarkServiceBlockingStub stub;

    private BlockingUnaryWorker(BenchmarkServiceGrpc.BenchmarkServiceBlockingStub stub) {
      this.stub = stub;
    }

    @Override
    public void run() {
      while (!shutdown) {
        long now = System.nanoTime();
        stub.unaryCall(simpleRequest);
        delay(System.nanoTime() - now);
      }
    }
  }

  /**
   * Worker which executes async unary calls. Event timing is the duration between sending the
   * request and receiving the response.
   */
  private class AsyncUnaryWorker implements Runnable {
    final BenchmarkServiceGrpc.BenchmarkServiceStub stub;
    final Semaphore maxOutstanding = new Semaphore(config.getOutstandingRpcsPerChannel());

    AsyncUnaryWorker(BenchmarkServiceGrpc.BenchmarkServiceStub stub) {
      this.stub = stub;
    }

    @Override
    public void run() {
      while (true) {
        maxOutstanding.acquireUninterruptibly();
        if (shutdown) {
          maxOutstanding.release();
          return;
        }
        stub.unaryCall(simpleRequest, new StreamObserver<Messages.SimpleResponse>() {
          long now = System.nanoTime();
          @Override
          public void onNext(Messages.SimpleResponse value) {

          }

          @Override
          public void onError(Throwable t) {
            maxOutstanding.release();
            Level level = shutdown ? Level.FINE : Level.INFO;
            log.log(level, "Error in AsyncUnary call", t);
          }

          @Override
          public void onCompleted() {
            delay(System.nanoTime() - now);
            maxOutstanding.release();
          }
        });
      }
    }
  }

  /**
   * Worker which executes a streaming ping-pong call. Event timing is the duration between
   * sending the ping and receiving the pong.
   */
  private class AsyncPingPongWorker implements Runnable {
    final BenchmarkServiceGrpc.BenchmarkServiceStub stub;
    final Semaphore maxOutstanding = new Semaphore(config.getOutstandingRpcsPerChannel());

    AsyncPingPongWorker(BenchmarkServiceGrpc.BenchmarkServiceStub stub) {
      this.stub = stub;
    }

    @Override
    public void run() {
      while (!shutdown) {
        maxOutstanding.acquireUninterruptibly();
        final AtomicReference<StreamObserver<Messages.SimpleRequest>> requestObserver =
            new AtomicReference<>();
        requestObserver.set(stub.streamingCall(
            new StreamObserver<Messages.SimpleResponse>() {
              long now = System.nanoTime();

              @Override
              public void onNext(Messages.SimpleResponse value) {
                delay(System.nanoTime() - now);
                if (shutdown) {
                  requestObserver.get().onCompleted();
                  // Must not send another request.
                  return;
                }
                requestObserver.get().onNext(simpleRequest);
                now = System.nanoTime();
              }

              @Override
              public void onError(Throwable t) {
                maxOutstanding.release();
                Level level = shutdown ? Level.FINE : Level.INFO;
                log.log(level, "Error in Async Ping-Pong call", t);

              }

              @Override
              public void onCompleted() {
                maxOutstanding.release();
              }
            }));
        requestObserver.get().onNext(simpleRequest);
      }
    }
  }

  /**
   * Worker which executes generic blocking unary calls. Event timing is the duration between
   * sending the request and receiving the response.
   */
  private class GenericBlockingUnaryWorker implements Runnable {
    final Channel channel;

    GenericBlockingUnaryWorker(Channel channel) {
      this.channel = channel;
    }

    @Override
    public void run() {
      long now;
      while (!shutdown) {
        now = System.nanoTime();
        ClientCalls.blockingUnaryCall(channel, LoadServer.GENERIC_UNARY_METHOD,
            CallOptions.DEFAULT,
            genericRequest.slice());
        delay(System.nanoTime() - now);
      }
    }
  }

  /**
   * Worker which executes generic async unary calls. Event timing is the duration between
   * sending the request and receiving the response.
   */
  private class GenericAsyncUnaryWorker implements Runnable {
    final Channel channel;
    final Semaphore maxOutstanding = new Semaphore(config.getOutstandingRpcsPerChannel());

    GenericAsyncUnaryWorker(Channel channel) {
      this.channel = channel;
    }

    @Override
    public void run() {
      while (true) {
        maxOutstanding.acquireUninterruptibly();
        if (shutdown) {
          maxOutstanding.release();
          return;
        }
        ClientCalls.asyncUnaryCall(
            channel.newCall(LoadServer.GENERIC_UNARY_METHOD, CallOptions.DEFAULT),
            genericRequest.slice(),
            new StreamObserver<ByteBuf>() {
              long now = System.nanoTime();

              @Override
              public void onNext(ByteBuf value) {

              }

              @Override
              public void onError(Throwable t) {
                maxOutstanding.release();
                Level level = shutdown ? Level.FINE : Level.INFO;
                log.log(level, "Error in Generic Async Unary call", t);
              }

              @Override
              public void onCompleted() {
                delay(System.nanoTime() - now);
                maxOutstanding.release();
              }
            });
      }
    }
  }

  /**
   * Worker which executes a streaming ping-pong call. Event timing is the duration between
   * sending the ping and receiving the pong.
   */
  private class GenericAsyncPingPongWorker implements Runnable {
    final Semaphore maxOutstanding = new Semaphore(config.getOutstandingRpcsPerChannel());
    final Channel channel;

    GenericAsyncPingPongWorker(Channel channel) {
      this.channel = channel;
    }

    @Override
    public void run() {
      while (true) {
        maxOutstanding.acquireUninterruptibly();
        if (shutdown) {
          maxOutstanding.release();
          return;
        }
        final ClientCall<ByteBuf, ByteBuf> call =
            channel.newCall(LoadServer.GENERIC_STREAMING_PING_PONG_METHOD, CallOptions.DEFAULT);
        call.start(new ClientCall.Listener<ByteBuf>() {
          long now = System.nanoTime();

          @Override
          public void onMessage(ByteBuf message) {
            delay(System.nanoTime() - now);
            if (shutdown) {
              call.cancel("Shutting down", null);
              return;
            }
            call.request(1);
            call.sendMessage(genericRequest.slice());
            now = System.nanoTime();
          }

          @Override
          public void onClose(Status status, Metadata trailers) {
            maxOutstanding.release();
            Level level = shutdown ? Level.FINE : Level.INFO;
            if (!status.isOk() && status.getCode() != Status.Code.CANCELLED) {
              log.log(level, "Error in Generic Async Ping-Pong call", status.getCause());
            }
          }
        }, new Metadata());
        call.request(1);
        call.sendMessage(genericRequest.slice());
      }
    }
  }
}
