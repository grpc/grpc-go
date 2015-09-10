/*
 * Copyright 2015, Google Inc. All rights reserved.
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

package io.grpc.benchmarks.netty;

import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.CallOptions;
import io.grpc.ClientCall;
import io.grpc.Drainable;
import io.grpc.KnownLength;
import io.grpc.ManagedChannel;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodDescriptor.MethodType;
import io.grpc.Server;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerServiceDefinition;
import io.grpc.Status;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.stub.ClientCalls;
import io.grpc.stub.StreamObserver;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.EmptyByteBuf;
import io.netty.buffer.PooledByteBufAllocator;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;
import io.netty.channel.nio.NioEventLoopGroup;

import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.NetworkInterface;
import java.net.ServerSocket;
import java.net.SocketAddress;
import java.net.SocketException;
import java.net.UnknownHostException;
import java.util.Enumeration;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Abstract base class for Netty end-to-end benchmarks.
 */
public abstract class AbstractBenchmark {

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

  private static final InetAddress BENCHMARK_ADDR;

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
  static {
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
    BENCHMARK_ADDR = tmp;
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
      serverBuilder.executor(MoreExecutors.newDirectExecutorService());
    }
    if (clientExecutor == ExecutorType.DIRECT) {
      channelBuilder.executor(MoreExecutors.newDirectExecutorService());
    }

    // Always use a different worker group from the client.
    serverBuilder.workerEventLoopGroup(new NioEventLoopGroup());

    // Always set connection and stream window size to same value
    serverBuilder.flowControlWindow(windowSize.bytes());
    channelBuilder.flowControlWindow(windowSize.bytes());

    channelBuilder.negotiationType(NegotiationType.PLAINTEXT);
    serverBuilder.maxConcurrentCallsPerConnection(maxConcurrentStreams);

    // Create buffers of the desired size for requests and responses.
    PooledByteBufAllocator alloc = PooledByteBufAllocator.DEFAULT;
    request = alloc.buffer(requestSize.bytes());
    request.writerIndex(request.capacity() - 1);
    response = alloc.buffer(responseSize.bytes());
    response.writerIndex(response.capacity() - 1);

    // Simple method that sends and receives NettyByteBuf
    unaryMethod = MethodDescriptor.create(MethodType.UNARY,
        "benchmark/unary",
        new ByteBufOutputMarshaller(),
        new ByteBufOutputMarshaller());
    pingPongMethod = MethodDescriptor.create(MethodType.BIDI_STREAMING,
        "benchmark/pingPong",
        new ByteBufOutputMarshaller(),
        new ByteBufOutputMarshaller());
    flowControlledStreaming = MethodDescriptor.create(MethodType.BIDI_STREAMING,
        "benchmark/flowControlledStreaming",
        new ByteBufOutputMarshaller(),
        new ByteBufOutputMarshaller());

    // Server implementation of unary & streaming methods
    serverBuilder.addService(
        ServerServiceDefinition.builder("benchmark")
            .addMethod(unaryMethod,
                new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      MethodDescriptor<ByteBuf, ByteBuf> method,
                      final ServerCall<ByteBuf> call,
                      Metadata headers) {
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
            .addMethod(pingPongMethod,
                new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      MethodDescriptor<ByteBuf, ByteBuf> method,
                      final ServerCall<ByteBuf> call,
                      Metadata headers) {
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
            .addMethod(flowControlledStreaming,
                new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(
                      MethodDescriptor<ByteBuf, ByteBuf> method,
                      final ServerCall<ByteBuf> call,
                      Metadata headers) {
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
    for (int i = 0; i < channelCount; i++) {
      // Use a dedicated event-loop for each channel
      channels[i] = channelBuilder
          .eventLoopGroup(new NioEventLoopGroup(1))
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
  protected void startStreamingCalls(int callsPerChannel,
                                     final AtomicLong counter,
                                     final AtomicBoolean done,
                                     final long counterDelta) {
    for (final ManagedChannel channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        final ClientCall<ByteBuf, ByteBuf> streamingCall =
            channel.newCall(pingPongMethod, CALL_OPTIONS);
        final AtomicReference<StreamObserver<ByteBuf>> requestObserverRef =
            new AtomicReference<StreamObserver<ByteBuf>>();
        StreamObserver<ByteBuf> requestObserver = ClientCalls.asyncBidiStreamingCall(
            streamingCall,
            new StreamObserver<ByteBuf>() {
              @Override
              public void onNext(ByteBuf value) {
                if (!done.get()) {
                  counter.addAndGet(counterDelta);
                  requestObserverRef.get().onNext(request.slice());
                  streamingCall.request(1);
                }
              }

              @Override
              public void onError(Throwable t) {
                done.set(true);
              }

              @Override
              public void onCompleted() {
              }
            });
        requestObserverRef.set(requestObserver);
        requestObserver.onNext(request.slice());
        requestObserver.onNext(request.slice());
      }
    }
  }

  /**
   * Start a continuously executing set of duplex streaming ping-pong calls that will terminate when
   * {@code done.get()} is true. Each completed call will increment the counter by the specified
   * delta which benchmarks can use to measure messages per second or bandwidth.
   */
  protected void startFlowControlledStreamingCalls(int callsPerChannel,
                                                   final AtomicLong counter,
                                                   final AtomicBoolean done,
                                                   final long counterDelta) {
    for (final ManagedChannel channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        final ClientCall<ByteBuf, ByteBuf> streamingCall =
            channel.newCall(flowControlledStreaming, CALL_OPTIONS);
        final AtomicReference<StreamObserver<ByteBuf>> requestObserverRef =
            new AtomicReference<StreamObserver<ByteBuf>>();
        StreamObserver<ByteBuf> requestObserver = ClientCalls.asyncBidiStreamingCall(
            streamingCall,
            new StreamObserver<ByteBuf>() {
              @Override
              public void onNext(ByteBuf value) {
                if (!done.get()) {
                  counter.addAndGet(counterDelta);
                  streamingCall.request(1);
                }
              }

              @Override
              public void onError(Throwable t) {
                done.set(true);
              }

              @Override
              public void onCompleted() {
              }
            });
        requestObserverRef.set(requestObserver);
        requestObserver.onNext(request.slice());
      }
    }
  }

  /**
   * Shutdown all the client channels and then shutdown the server.
   */
  protected void teardown() throws Exception {
    for (ManagedChannel channel : channels) {
      channel.shutdown();
    }
    server.shutdown().awaitTermination(5, TimeUnit.SECONDS);
  }

  /**
   * Simple {@link io.grpc.MethodDescriptor.Marshaller} for Netty ByteBuf.
   */
  protected static class ByteBufOutputMarshaller implements MethodDescriptor.Marshaller<ByteBuf> {

    public static final EmptyByteBuf EMPTY_BYTE_BUF =
        new EmptyByteBuf(PooledByteBufAllocator.DEFAULT);

    protected ByteBufOutputMarshaller() {
    }

    @Override
    public InputStream stream(ByteBuf value) {
      return new ByteBufInputStream(value);
    }

    @Override
    public ByteBuf parse(InputStream stream) {
      try {
        // We don't do anything with the message and it's already been read into buffers
        // so just skip copying it.
        stream.skip(stream.available());
        return EMPTY_BYTE_BUF;
      } catch (IOException ioe) {
        throw new RuntimeException(ioe);
      }
    }
  }

  /**
   * A {@link Drainable} {@code InputStream} that reads an {@link io.netty.buffer.ByteBuf}.
   */
  private static class ByteBufInputStream extends InputStream
      implements Drainable, KnownLength {

    private ByteBuf buf;

    private ByteBufInputStream(ByteBuf buf) {
      this.buf = buf;
    }

    @Override
    public int drainTo(OutputStream target) throws IOException {
      int readbableBytes = buf.readableBytes();
      buf.readBytes(target, readbableBytes);
      buf = null;
      return readbableBytes;
    }

    @Override
    public int available() throws IOException {
      if (buf != null) {
        return buf.readableBytes();
      }
      return 0;
    }

    @Override
    public int read() throws IOException {
      throw new UnsupportedOperationException();
    }
  }
}
