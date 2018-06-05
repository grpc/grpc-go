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

package io.grpc.benchmarks;

import static io.grpc.benchmarks.Utils.pickUnusedPort;

import com.google.protobuf.ByteString;
import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.benchmarks.proto.BenchmarkServiceGrpc;
import io.grpc.benchmarks.proto.Messages.Payload;
import io.grpc.benchmarks.proto.Messages.SimpleRequest;
import io.grpc.benchmarks.proto.Messages.SimpleResponse;
import io.grpc.benchmarks.qps.AsyncServer;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.AbstractServerImplBuilder;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.netty.channel.Channel;
import io.netty.channel.EventLoopGroup;
import io.netty.channel.ServerChannel;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;
import java.net.InetSocketAddress;
import java.util.concurrent.Future;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

/** Some text. */
@State(Scope.Benchmark)
public class TransportBenchmark {
  public enum Transport {
    INPROCESS, NETTY, NETTY_LOCAL, NETTY_EPOLL, OKHTTP
  }

  @Param({"INPROCESS", "NETTY", "NETTY_LOCAL", "OKHTTP"})
  public Transport transport;
  @Param({"true", "false"})
  public boolean direct;

  private ManagedChannel channel;
  private Server server;
  private BenchmarkServiceGrpc.BenchmarkServiceBlockingStub stub;
  private volatile EventLoopGroup groupToShutdown;

  @Setup
  @SuppressWarnings("LiteralClassName") // Epoll is not available on windows
  public void setUp() throws Exception {
    AbstractServerImplBuilder<?> serverBuilder;
    AbstractManagedChannelImplBuilder<?> channelBuilder;
    switch (transport) {
      case INPROCESS:
      {
        String name = "bench" + Math.random();
        serverBuilder = InProcessServerBuilder.forName(name);
        channelBuilder = InProcessChannelBuilder.forName(name);
        break;
      }
      case NETTY:
      {
        InetSocketAddress address = new InetSocketAddress("localhost", pickUnusedPort());
        serverBuilder = NettyServerBuilder.forAddress(address);
        channelBuilder = NettyChannelBuilder.forAddress(address)
            .negotiationType(NegotiationType.PLAINTEXT);
        break;
      }
      case NETTY_LOCAL:
      {
        String name = "bench" + Math.random();
        LocalAddress address = new LocalAddress(name);
        serverBuilder = NettyServerBuilder.forAddress(address)
            .channelType(LocalServerChannel.class);
        channelBuilder = NettyChannelBuilder.forAddress(address)
            .channelType(LocalChannel.class)
            .negotiationType(NegotiationType.PLAINTEXT);
        break;
      }
      case NETTY_EPOLL:
      {
        InetSocketAddress address = new InetSocketAddress("localhost", pickUnusedPort());

        // Reflection used since they are only available on linux.
        Class<?> groupClass = Class.forName("io.netty.channel.epoll.EpollEventLoopGroup");
        EventLoopGroup group = (EventLoopGroup) groupClass.getConstructor().newInstance();

        @SuppressWarnings("unchecked")
        Class<? extends ServerChannel> serverChannelClass = (Class<? extends ServerChannel>)
            Class.forName("io.netty.channel.epoll.EpollServerSocketChannel");
        serverBuilder = NettyServerBuilder.forAddress(address)
            .bossEventLoopGroup(group)
            .workerEventLoopGroup(group)
            .channelType(serverChannelClass);
        @SuppressWarnings("unchecked")
        Class<? extends Channel> channelClass = (Class<? extends Channel>)
            Class.forName("io.netty.channel.epoll.EpollSocketChannel");
        channelBuilder = NettyChannelBuilder.forAddress(address)
            .eventLoopGroup(group)
            .channelType(channelClass)
            .negotiationType(NegotiationType.PLAINTEXT);
        groupToShutdown = group;
        break;
      }
      case OKHTTP:
      {
        int port = pickUnusedPort();
        InetSocketAddress address = new InetSocketAddress("localhost", port);
        serverBuilder = NettyServerBuilder.forAddress(address);
        channelBuilder = OkHttpChannelBuilder.forAddress("localhost", port).usePlaintext();
        break;
      }
      default:
        throw new Exception("Unknown transport: " + transport);
    }

    if (direct) {
      serverBuilder.directExecutor();
      // Because blocking stubs avoid the executor, this doesn't do much.
      channelBuilder.directExecutor();
    }

    server = serverBuilder
        .addService(new AsyncServer.BenchmarkServiceImpl())
        .build();
    server.start();
    channel = channelBuilder.build();
    stub = BenchmarkServiceGrpc.newBlockingStub(channel);
    // Wait for channel to start
    stub.unaryCall(SimpleRequest.getDefaultInstance());
  }

  @TearDown
  public void tearDown() throws Exception {
    channel.shutdown();
    server.shutdown();
    channel.awaitTermination(1, TimeUnit.SECONDS);
    server.awaitTermination(1, TimeUnit.SECONDS);
    if (!channel.isTerminated()) {
      throw new Exception("failed to shut down channel");
    }
    if (!server.isTerminated()) {
      throw new Exception("failed to shut down server");
    }
    if (groupToShutdown != null) {
      Future<?> unused = groupToShutdown.shutdownGracefully(0, 1, TimeUnit.SECONDS);
      groupToShutdown.awaitTermination(1, TimeUnit.SECONDS);
      if (!groupToShutdown.isTerminated()) {
        throw new Exception("failed to shut down event loop group.");
      }
    }
  }

  private SimpleRequest simpleRequest = SimpleRequest.newBuilder()
      .setResponseSize(1024)
      .setPayload(Payload.newBuilder().setBody(ByteString.copyFrom(new byte[1024])))
      .build();

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public SimpleResponse unaryCall1024() {
    return stub.unaryCall(simpleRequest);
  }
}
