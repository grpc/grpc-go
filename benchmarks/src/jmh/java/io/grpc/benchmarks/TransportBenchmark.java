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

package io.grpc.benchmarks;

import static io.grpc.testing.TestUtils.pickUnusedPort;

import com.google.common.util.concurrent.MoreExecutors;
import com.google.protobuf.ByteString;

import io.grpc.ManagedChannel;
import io.grpc.Server;
import io.grpc.benchmarks.qps.AsyncServer;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.internal.AbstractManagedChannelImplBuilder;
import io.grpc.internal.AbstractServerImplBuilder;
import io.grpc.netty.NegotiationType;
import io.grpc.netty.NettyChannelBuilder;
import io.grpc.netty.NettyServerBuilder;
import io.grpc.okhttp.OkHttpChannelBuilder;
import io.grpc.testing.Payload;
import io.grpc.testing.SimpleRequest;
import io.grpc.testing.SimpleResponse;
import io.grpc.testing.TestServiceGrpc;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;

import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;
import org.openjdk.jmh.annotations.TearDown;

import java.net.InetSocketAddress;
import java.util.concurrent.TimeUnit;

/** Some text. */
@State(Scope.Benchmark)
public class TransportBenchmark {
  public enum Transport {
    INPROCESS, NETTY, NETTY_LOCAL, OKHTTP
  }

  @Param({"INPROCESS", "NETTY", "NETTY_LOCAL", "OKHTTP"})
  public Transport transport;
  @Param({"true", "false"})
  public boolean direct;

  private ManagedChannel channel;
  private Server server;
  private TestServiceGrpc.TestServiceBlockingStub stub;

  @Setup
  public void setUp() throws Exception {
    AbstractServerImplBuilder serverBuilder;
    AbstractManagedChannelImplBuilder channelBuilder;
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
      case OKHTTP:
      {
        int port = pickUnusedPort();
        InetSocketAddress address = new InetSocketAddress("localhost", port);
        serverBuilder = NettyServerBuilder.forAddress(address);
        channelBuilder = OkHttpChannelBuilder.forAddress("localhost", port)
            .negotiationType(io.grpc.okhttp.NegotiationType.PLAINTEXT);
        break;
      }
      default:
        throw new Exception("Unknown transport: " + transport);
    }

    if (direct) {
      serverBuilder.executor(MoreExecutors.newDirectExecutorService());
      channelBuilder.executor(MoreExecutors.newDirectExecutorService());
    }

    server = serverBuilder
        .addService(TestServiceGrpc.bindService(new AsyncServer.TestServiceImpl()))
        .build();
    server.start();
    channel = channelBuilder.build();
    stub = TestServiceGrpc.newBlockingStub(channel);
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
