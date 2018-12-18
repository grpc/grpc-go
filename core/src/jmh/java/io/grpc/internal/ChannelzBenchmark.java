/*
 * Copyright 2018 The gRPC Authors
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

package io.grpc.internal;

import com.google.common.util.concurrent.ListenableFuture;
import io.grpc.InternalChannelz;
import io.grpc.InternalChannelz.ServerStats;
import io.grpc.InternalChannelz.SocketStats;
import io.grpc.InternalInstrumented;
import io.grpc.InternalLogId;
import java.util.concurrent.TimeUnit;
import org.openjdk.jmh.annotations.Benchmark;
import org.openjdk.jmh.annotations.BenchmarkMode;
import org.openjdk.jmh.annotations.Mode;
import org.openjdk.jmh.annotations.OutputTimeUnit;
import org.openjdk.jmh.annotations.Param;
import org.openjdk.jmh.annotations.Scope;
import org.openjdk.jmh.annotations.Setup;
import org.openjdk.jmh.annotations.State;

/**
 * Javadoc.
 */
@State(Scope.Benchmark)
public class ChannelzBenchmark {
  // Number of items already present
  @Param({"10", "100", "1000", "10000"})
  public int preexisting;

  public InternalChannelz channelz = new InternalChannelz();

  public InternalInstrumented<ServerStats> serverToRemove;

  public InternalInstrumented<ServerStats> serverToAdd;

  public InternalInstrumented<ServerStats> serverForServerSocket;
  public InternalInstrumented<SocketStats> serverSocketToAdd;
  public InternalInstrumented<SocketStats> serverSocketToRemove;

  /**
   * Javadoc.
   */
  @Setup
  @SuppressWarnings({"unchecked", "rawtypes"})
  public void setUp() {
    serverToRemove = create();
    channelz.addServer(serverToRemove);

    serverForServerSocket = create();
    channelz.addServer(serverForServerSocket);

    serverSocketToRemove = create();
    channelz.addClientSocket(serverSocketToRemove);
    channelz.addServerSocket(serverForServerSocket, serverSocketToRemove);

    populate(preexisting);

    serverToAdd = create();
    serverSocketToAdd = create();
  }

  private void populate(int count) {
    for (int i = 0; i < count; i++) {
      // for addNavigable / removeNavigable
      InternalInstrumented<ServerStats> srv = create();
      channelz.addServer(srv);

      // for add / remove
      InternalInstrumented<SocketStats> sock = create();
      channelz.addClientSocket(sock);

      // for addServerSocket / removeServerSocket
      channelz.addServerSocket(serverForServerSocket, sock);
    }
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void addNavigable() {
    channelz.addServer(serverToAdd);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void add() {
    channelz.addClientSocket(serverSocketToAdd);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void addServerSocket() {
    channelz.addServerSocket(serverForServerSocket, serverSocketToAdd);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void removeNavigable() {
    channelz.removeServer(serverToRemove);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void remove() {
    channelz.removeClientSocket(serverSocketToRemove);
  }

  @Benchmark
  @BenchmarkMode(Mode.SampleTime)
  @OutputTimeUnit(TimeUnit.NANOSECONDS)
  public void removeServerSocket() {
    channelz.removeServerSocket(serverForServerSocket, serverSocketToRemove);
  }

  private static <T> InternalInstrumented<T> create() {
    return new InternalInstrumented<T>() {
      final InternalLogId id = InternalLogId.allocate(getClass(), "fake-tag");

      @Override
      public ListenableFuture<T> getStats() {
        throw new UnsupportedOperationException();
      }

      @Override
      public InternalLogId getLogId() {
        return id;
      }
    };
  }
}
