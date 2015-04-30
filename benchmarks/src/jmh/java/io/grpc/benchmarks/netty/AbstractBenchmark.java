package io.grpc.benchmarks.netty;

import com.google.common.util.concurrent.MoreExecutors;

import io.grpc.ChannelImpl;
import io.grpc.DeferredInputStream;
import io.grpc.Marshaller;
import io.grpc.Metadata;
import io.grpc.MethodDescriptor;
import io.grpc.MethodType;
import io.grpc.ServerCall;
import io.grpc.ServerCallHandler;
import io.grpc.ServerImpl;
import io.grpc.ServerServiceDefinition;
import io.grpc.Status;
import io.grpc.stub.Calls;
import io.grpc.stub.StreamObserver;
import io.grpc.transport.netty.NegotiationType;
import io.grpc.transport.netty.NettyChannelBuilder;
import io.grpc.transport.netty.NettyServerBuilder;
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
import java.net.InetSocketAddress;
import java.net.ServerSocket;
import java.net.SocketAddress;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import javax.annotation.Nullable;

/**
 * Abstract base class for Netty end-to-end benchmarks.
 */
public abstract class AbstractBenchmark {

  /**
   * Standard payload sizes.
   */
  public enum PayloadSize {
    SMALL(10), MEDIUM(1024), LARGE(65536), JUMBO(16777216);

    private final int bytes;
    PayloadSize(int bytes) {
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
    SMALL(16384), MEDIUM(65536), LARGE(1048576), JUMBO(16777216);

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


  protected ServerImpl server;
  protected ByteBuf request;
  protected ByteBuf response;
  protected MethodDescriptor<ByteBuf, ByteBuf> unaryMethod;
  protected ChannelImpl[] channels;

  public AbstractBenchmark() {
  }

  /**
   * Initialize the environment for the executor.
   */
  public void setup(ExecutorType clientExecutor,
      ExecutorType serverExecutor,
      PayloadSize requestSize,
      PayloadSize responseSize,
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
      // Pick a port using an ephemeral socket.
      ServerSocket sock = new ServerSocket();
      sock.bind(new InetSocketAddress("localhost", 0));
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
    serverBuilder.connectionWindowSize(windowSize.bytes());
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
        5,
        TimeUnit.SECONDS,
        new ByteBufOutputMarshaller(),
        new ByteBufOutputMarshaller());

    // Server implementation of same method
    serverBuilder.addService(
        ServerServiceDefinition.builder("benchmark")
            .addMethod("unary",
                new ByteBufOutputMarshaller(),
                new ByteBufOutputMarshaller(),
                new ServerCallHandler<ByteBuf, ByteBuf>() {
                  @Override
                  public ServerCall.Listener<ByteBuf> startCall(String fullMethodName,
                                                                final ServerCall<ByteBuf> call,
                                                                Metadata.Headers headers) {
                    call.request(1);
                    return new ServerCall.Listener<ByteBuf>() {
                      @Override
                      public void onPayload(ByteBuf payload) {
                        // no-op
                        payload.release();
                        call.sendPayload(response.slice());
                      }

                      @Override
                      public void onHalfClose() {
                        call.close(Status.OK, new Metadata.Trailers());
                      }

                      @Override
                      public void onCancel() {

                      }

                      @Override
                      public void onComplete() {
                      }
                    };
                  }
                }).build());

    // Build and start the clients and servers
    server = serverBuilder.build();
    server.start();
    channels = new ChannelImpl[channelCount];
    for (int i = 0; i < channelCount; i++) {
      // Use a dedicated event-loop for each channel
      channels[i] = channelBuilder
          .eventLoopGroup(new NioEventLoopGroup(1))
          .build();
    }
  }

  /**
   * Start a continuously executing set of unary calls that will terminate when
   * {@code done.get()} is true. Each completed call will increment the counter
   * which benchmarks can check for progress.
   */
  protected void startUnaryCalls(int callsPerChannel,
                              final AtomicLong counter,
                              final AtomicBoolean done) {
    for (final ChannelImpl channel : channels) {
      for (int i = 0; i < callsPerChannel; i++) {
        StreamObserver<ByteBuf> observer = new StreamObserver<ByteBuf>() {
          @Override
          public void onValue(ByteBuf value) {
            counter.incrementAndGet();
          }

          @Override
          public void onError(Throwable t) {
            done.set(true);
          }

          @Override
          public void onCompleted() {
            if (!done.get()) {
              ByteBuf slice = request.slice();
              Calls.asyncUnaryCall(channel.newCall(unaryMethod), slice, this);
            }
          }
        };
        observer.onCompleted();
      }
    }
  }

  protected void stopChannelsAndServers() throws Exception {
    for (ChannelImpl channel : channels) {
      channel.shutdown();
    }
    server.shutdown().awaitTerminated(5, TimeUnit.SECONDS);
  }

  /**
   * Simple {@link io.grpc.Marshaller} for Netty ByteBuf.
   */
  protected static class ByteBufOutputMarshaller implements Marshaller<ByteBuf> {

    public static final EmptyByteBuf EMPTY_BYTE_BUF =
        new EmptyByteBuf(PooledByteBufAllocator.DEFAULT);

    protected ByteBufOutputMarshaller() {
    }

    @Override
    public InputStream stream(ByteBuf value) {
      return new DeferredByteBufInputStream(value);
    }

    @Override
    public ByteBuf parse(InputStream stream) {
      try {
        // We don't do anything with the payload and it's already been read into buffers
        // so just skip copying it.
        stream.skip(stream.available());
        return EMPTY_BYTE_BUF;
      } catch (IOException ioe) {
        throw new RuntimeException(ioe);
      }
    }
  }

  /**
   * Implementation of {@link io.grpc.DeferredInputStream} for {@link io.netty.buffer.ByteBuf}.
   */
  private static class DeferredByteBufInputStream extends DeferredInputStream<ByteBuf> {

    private ByteBuf buf;

    private DeferredByteBufInputStream(ByteBuf buf) {
      this.buf = buf;
    }

    @Override
    public int flushTo(OutputStream target) throws IOException {
      int readbableBytes = buf.readableBytes();
      buf.readBytes(target, readbableBytes);
      buf = null;
      return readbableBytes;
    }

    @Nullable
    @Override
    public ByteBuf getDeferred() {
      return buf;
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
