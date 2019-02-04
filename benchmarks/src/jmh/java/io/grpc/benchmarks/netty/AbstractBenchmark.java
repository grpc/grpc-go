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

package io.grpc.benchmarks.netty;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Server;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.ServiceDescriptor;
import io.grpc.Status;
import io.grpc.benchmarks.ByteBufOutputMarshaller;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.ClientCalls;
import io.grpc.stub.StreamObserver;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.PooledByteBufAllocator;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;
import io.netty.channel.nio.NioEventLoopGroup;
import io.netty.util.concurrent.DefaultThreadFactory;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.NetworkInterface;
import java.net.ServerSocket;
import java.net.SocketAddress;
import java.net.SocketException;
import java.net.UnknownHostException;
import java.util.Enumeration;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;
import java.util.logging.Level;
import java.util.logging.Logger;

/**
 * Abstract base class for Netty end-to-end benchmarks.
 */
public abstract class AbstractBenchmark {

  private static final Logger logger = Logger.getLogger(AbstractBenchmark.class.getName());

  /**
   * Standard message sizes.
   */
  public enum MessageSize {
    // Max out at 1MB to avoid creating messages larger than Netty's buffer pool can handle
    // by default
    SMALL(10), MEDIUM(1024), LARGE(65536), JUMBO(1048576);

    private final int bytes;
    MessageSize(int bytes) {
      this.bytes = bytes;
    }

    public int bytes() {
      return bytes;
    }
  }

  /**
   * Standard flow-control window sizes.
   */
  public enum FlowWindowSize {
    SMALL(16383), MEDIUM(65535), LARGE(1048575), JUMBO(8388607);

    private final int bytes;
    FlowWindowSize(int bytes) {
      this.bytes = bytes;
    }

    public int bytes() {
      return bytes;
    }
  }

  /**
   * Executor types used by Channel & Server.
   */
  public enum ExecutorType {
    DEFAULT, DIRECT;
  }

  /**
   * Support channel types.
   */
  public enum ChannelType {
    NIO, LOCAL;
  }

  private static final CallOptions CALL_OPTIONS = CallOptions.DEFAULT;

  private static final InetAddress BENCHMARK_ADDR = buildBenchmarkAddr();

  /**
   * Resolve the address bound to the benchmark interface. Currently we assume it's a
   * child interface of the loopback interface with the term 'benchmark' in its name.
   *
   * <p>>This allows traffic shaping to be applied to an IP address and to have the benchmarks
   * detect it's presence and use it. E.g for Linux we can apply netem to a specific IP to
   * do traffic shaping, bind that IP to the loopback adapter and then apply a label to that
   * binding so that it appears as a child interface.
   *
   * <pre>
   * sudo tc qdisc del dev lo root
   * sudo tc qdisc add dev lo root handle 1: prio
   * sudo tc qdisc add dev lo parent 1:1 handle 2: netem delay 0.1ms rate 10gbit
   * sudo tc filter add dev lo parent 1:0 protocol ip prio 1  \
   *            u32 match ip dst 127.127.127.127 flowid 2:1
   * sudo ip addr add dev lo 127.127.127.127/32 label lo:benchmark
   * </pre>
   */
  private static InetAddress buildBenchmarkAddr() {
    InetAddress tmp = null;
    try {
      Enumeration<NetworkInterface> networkInterfaces = NetworkInterface.getNetworkInterfaces();
      outer: while (networkInterfaces.hasMoreElements()) {
        NetworkInterface networkInterface = networkInterfaces.nextElement();
        if (!networkInterface.isLoopback()) {
          continue;
        }
        Enumeration<NetworkInterface> subInterfaces = networkInterface.getSubInterfaces();
        while (subInterfaces.hasMoreElements()) {
          NetworkInterface subLoopback = subInterfaces.nextElement();
          if (subLoopback.getDisplayName().contains("benchmark")) {
            tmp = subLoopback.getInetAddresses().nextElement();
            System.out.println("\nResolved benchmark address to " + tmp + " on "
                + subLoopback.getDisplayName() + "\n\n");
            break outer;
          }
        }
      }
    } catch (SocketException se) {
      System.out.println("\nWARNING: Error trying to resolve benchmark interface \n" +  se);
    }
    if (tmp == null) {
      try {
        System.out.println(
            "\nWARNING: Unable to resolve benchmark interface, defaulting to localhost");
        tmp = InetAddress.getLocalHost();
      } catch (UnknownHostException uhe) {
        throw new RuntimeException(uhe);
      }
    }
    return tmp;
  }

  protected Server server;
  protected ByteBuf request;
  protected ByteBuf response;
  protected MethodDescriptor<ByteBuf, ByteBuf> unaryMethod;
  private MethodDescriptor<ByteBuf, ByteBuf> pingPongMethod;
  private MethodDescriptor<ByteBuf, ByteBuf> flowControlledStreaming;
  protected ManagedChannel[] channels;

  public AbstractBenchmark() {
  }

  /**
   * Initialize the environment for the executor.
   */
  public void setup(ExecutorType clientExecutor,
                    ExecutorType serverExecutor,
                    MessageSize requestSize,
                    MessageSize responseSize,
                    FlowWindowSize windowSize,
                    ChannelType channelType,
                    int maxConcurrentStreams,
                    int channelCount) throws Exception {
    NettyServerBuilder serverBuilder;
    NettyChannelBuilder channelBuilder;
    if (channelType == ChannelType.LOCAL) {
      LocalAddress address = new LocalAddress("netty-e2e-benchmark");
      serverBuilder = NettyServerBuilder.forAddress(address);
      serverBuilder.channelType(LocalServerChannel.class);
      channelBuilder = NettyChannelBuilder.forAddress(address);
      channelBuilder.channelType(LocalChannel.class);
    } else {
      ServerSocket sock = new ServerSocket();
      // Pick a port using an ephemeral socket.
      sock.bind(new InetSocketAddress(BENCHMARK_ADDR, 0));
      SocketAddress address = sock.getLocalSocketAddress();
      sock.close();
      serverBuilder = NettyServerBuilder.forAddress(address);
      channelBuilder = NettyChannelBuilder.forAddress(address);
    }

    if (serverExecutor == ExecutorType.DIRECT) {
      serverBuilder.directExecutor();
    }
    if (clientExecutor == ExecutorType.DIRECT) {
      channelBuilder.directExecutor();
    }

    // Always use a different worker group from the client.
    ThreadFactory serverThreadFactory = new DefaultThreadFactory("STF pool", true /* daemon */);
    serverBuilder.workerEventLoopGroup(new NioEventLoopGroup(0, serverThreadFactory));

    // Always set connection and stream window size to same value
    serverBuilder.flowControlWindow(windowSize.bytes());
    channelBuilder.flowControlWindow(windowSize.bytes());

    channelBuilder.negotiationType(NegotiationType.PLAINTEXT);
    serverBuilder.maxConcurrentCallsPerConnection(maxConcurrentStreams);

    // Create buffers of the desired size for requests and responses.
    PooledByteBufAllocator alloc = PooledByteBufAllocator.DEFAULT;
    // Use a heap buffer for now, since MessageFramer doesn't know how to directly convert this
    // into a WritableBuffer
    // TODO(carl-mastrangelo): convert this into a regular buffer() call.  See
    // https://github.com/grpc/grpc-java/issues/2062#issuecomment-234646216
    request = alloc.heapBuffer(requestSize.bytes());
    request.writerIndex(request.capacity() - 1);
    response = alloc.heapBuffer(responseSize.bytes());
    response.writerIndex(response.capacity() - 1);

    // Simple method that sends and receives NettyByteBuf
    unaryMethod = MethodDescriptor.<ByteBuf, ByteBuf>newBuilder()
        .setType(MethodType.UNARY)
        .setFullMethodName("benchmark/unary")
        .setRequestMarshaller(new ByteBufOutputMarshaller())
        .setResponseMarshaller(new ByteBufOutputMarshaller())
        .build();

    pingPongMethod = unaryMethod.toBuilder()
        .setType(MethodType.BIDI_STREAMING)
        .setFullMethodName("benchmark/pingPong")
        .build();
    flowControlledStreaming = pingPongMethod.toBuilder()
        .setFullMethodName("benchmark/flowControlledStreaming")
        .build();

    // Server implementation of unary & streaming methods
    serverBuilder.addService(
        ServerServiceDefinition.builder(
            new ServiceDescriptor("benchmark",
                unaryMethod,
                pingPongMethod,
                flowControlledStreaming))
            .addMethod(unaryMethod, new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      final ServerCall<ByteBuf, ByteBuf> call,
                      Metadata headers) {
                    call.sendHeaders(new Metadata());
                    call.request(1);
                    return new ServerCall.Listener<ByteBuf>() {
                      @Override
                      public void onMessage(ByteBuf message) {
                        // no-op
                        message.release();
                        call.sendMessage(response.slice());
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
                })
            .addMethod(pingPongMethod, new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      final ServerCall<ByteBuf, ByteBuf> call,
                      Metadata headers) {
                    call.sendHeaders(new Metadata());
                    call.request(1);
                    return new ServerCall.Listener<ByteBuf>() {
                      @Override
                      public void onMessage(ByteBuf message) {
                        message.release();
                        call.sendMessage(response.slice());
                        // Request next message
                        call.request(1);
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
                })
            .addMethod(flowControlledStreaming, new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      final ServerCall<ByteBuf, ByteBuf> call,
                      Metadata headers) {
                    call.sendHeaders(new Metadata());
                    call.request(1);
                    return new ServerCall.Listener<ByteBuf>() {
                      @Override
                      public void onMessage(ByteBuf message) {
                        message.release();
                        while (call.isReady()) {
                          call.sendMessage(response.slice());
                        }
                        // Request next message
                        call.request(1);
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

                      @Override
                      public void onReady() {
                        while (call.isReady()) {
                          call.sendMessage(response.slice());
                        }
                      }
                    };
                  }
                })
            .build());

    // Build and start the clients and servers
    server = serverBuilder.build();
    server.start();
    channels = new ManagedChannel[channelCount];
    ThreadFactory clientThreadFactory = new DefaultThreadFactory("CTF pool", true /* daemon */);
    for (int i = 0; i < channelCount; i++) {
      // Use a dedicated event-loop for each channel
      channels[i] = channelBuilder
          .eventLoopGroup(new NioEventLoopGroup(1, clientThreadFactory))
          .build();
    }
  }

  /**
   * Start a continuously executing set of unary calls that will terminate when
   * {@code done.get()} is true. Each completed call will increment the counter by the specified
   * delta which benchmarks can use to measure QPS or bandwidth.
   */
  protected void startUnaryCalls(int callsPerChannel,
                                 final AtomicLong counter,
                                 final AtomicBoolean done,
                                 final long counterDelta) {
    for (final ManagedChannel channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        StreamObserver<ByteBuf> observer = new StreamObserver<ByteBuf>() {
          @Override
          public void onNext(ByteBuf value) {
            counter.addAndGet(counterDelta);
          }

          @Override
          public void onError(Throwable t) {
            done.set(true);
          }

          @Override
          public void onCompleted() {
            if (!done.get()) {
              ByteBuf slice = request.slice();
              ClientCalls.asyncUnaryCall(
                  channel.newCall(unaryMethod, CALL_OPTIONS), slice, this);
            }
          }
        };
        observer.onCompleted();
      }
    }
  }

  /**
   * Start a continuously executing set of duplex streaming ping-pong calls that will terminate when
   * {@code done.get()} is true. Each completed call will increment the counter by the specified
   * delta which benchmarks can use to measure messages per second or bandwidth.
   */
  protected CountDownLatch startStreamingCalls(int callsPerChannel, final AtomicLong counter,
      final AtomicBoolean record, final AtomicBoolean done, final long counterDelta) {
    final CountDownLatch latch = new CountDownLatch(callsPerChannel * channels.length);
    for (final ManagedChannel channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        final ClientCall<ByteBuf, ByteBuf> streamingCall =
            channel.newCall(pingPongMethod, CALL_OPTIONS);
        final AtomicReference<StreamObserver<ByteBuf>> requestObserverRef =
            new AtomicReference<>();
        final AtomicBoolean ignoreMessages = new AtomicBoolean();
        StreamObserver<ByteBuf> requestObserver = ClientCalls.asyncBidiStreamingCall(
            streamingCall,
            new StreamObserver<ByteBuf>() {
              @Override
              public void onNext(ByteBuf value) {
                if (done.get()) {
                  if (!ignoreMessages.getAndSet(true)) {
                    requestObserverRef.get().onCompleted();
                  }
                  return;
                }
                requestObserverRef.get().onNext(request.slice());
                if (record.get()) {
                  counter.addAndGet(counterDelta);
                }
                // request is called automatically because the observer implicitly has auto
                // inbound flow control
              }

              @Override
              public void onError(Throwable t) {
                logger.log(Level.WARNING, "call error", t);
                latch.countDown();
              }

              @Override
              public void onCompleted() {
                latch.countDown();
              }
            });
        requestObserverRef.set(requestObserver);
        requestObserver.onNext(request.slice());
        requestObserver.onNext(request.slice());
      }
    }
    return latch;
  }

  /**
   * Start a continuously executing set of duplex streaming ping-pong calls that will terminate when
   * {@code done.get()} is true. Each completed call will increment the counter by the specified
   * delta which benchmarks can use to measure messages per second or bandwidth.
   */
  protected CountDownLatch startFlowControlledStreamingCalls(int callsPerChannel,
      final AtomicLong counter, final AtomicBoolean record, final AtomicBoolean done,
      final long counterDelta) {
    final CountDownLatch latch = new CountDownLatch(callsPerChannel * channels.length);
    for (final ManagedChannel channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        final ClientCall<ByteBuf, ByteBuf> streamingCall =
            channel.newCall(flowControlledStreaming, CALL_OPTIONS);
        final AtomicReference<StreamObserver<ByteBuf>> requestObserverRef =
            new AtomicReference<>();
        final AtomicBoolean ignoreMessages = new AtomicBoolean();
        StreamObserver<ByteBuf> requestObserver = ClientCalls.asyncBidiStreamingCall(
            streamingCall,
            new StreamObserver<ByteBuf>() {
              @Override
              public void onNext(ByteBuf value) {
                StreamObserver<ByteBuf> obs = requestObserverRef.get();
                if (done.get()) {
                  if (!ignoreMessages.getAndSet(true)) {
                    obs.onCompleted();
                  }
                  return;
                }
                if (record.get()) {
                  counter.addAndGet(counterDelta);
                }
                // request is called automatically because the observer implicitly has auto
                // inbound flow control
              }

              @Override
              public void onError(Throwable t) {
                logger.log(Level.WARNING, "call error", t);
                latch.countDown();
              }

              @Override
              public void onCompleted() {
                latch.countDown();
              }
            });
        requestObserverRef.set(requestObserver);

        // Add some outstanding requests to ensure the server is filling the connection
        streamingCall.request(5);
        requestObserver.onNext(request.slice());
      }
    }
    return latch;
  }

  /**
   * Shutdown all the client channels and then shutdown the server.
   */
  protected void teardown() throws Exception {
    logger.fine("shutting down channels");
    for (ManagedChannel channel : channels) {
      channel.shutdown();
    }
    logger.fine("shutting down server");
    server.shutdown();
    if (!server.awaitTermination(5, TimeUnit.SECONDS)) {
      logger.warning("Failed to shutdown server");
    }
    logger.fine("server shut down");
    for (ManagedChannel channel : channels) {
      if (!channel.awaitTermination(1, TimeUnit.SECONDS)) {
        logger.warning("Failed to shutdown client");
      }
    }
    logger.fine("channels shut down");
  }
}
