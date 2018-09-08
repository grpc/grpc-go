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

package io.grpc.netty;

import io.grpc.ServerStreamTracer;
import io.grpc.internal.ClientTransportFactory;
import io.grpc.internal.FakeClock;
import io.grpc.internal.InternalServer;
import io.grpc.internal.ManagedClientTransport;
import io.grpc.internal.testing.AbstractTransportTest;
import java.net.InetSocketAddress;
import java.util.List;
import java.util.concurrent.TimeUnit;
import org.junit.After;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/** Unit tests for Netty transport. */
@RunWith(JUnit4.class)
public class NettyTransportTest extends AbstractTransportTest {
  private final FakeClock fakeClock = new FakeClock();
  // Avoid LocalChannel for testing because LocalChannel can fail with
  // io.netty.channel.ChannelException instead of java.net.ConnectException which breaks
  // serverNotListening test.
  private final ClientTransportFactory clientFactory = NettyChannelBuilder
      // Although specified here, address is ignored because we never call build.
      .forAddress("localhost", 0)
      .flowControlWindow(65 * 1024)
      .negotiationType(NegotiationType.PLAINTEXT)
      .setTransportTracerFactory(fakeClockTransportTracer)
      .buildTransportFactory();

  @Override
  protected boolean haveTransportTracer() {
    return true;
  }

  @After
  public void releaseClientFactory() {
    clientFactory.close();
  }

  @Override
  protected InternalServer newServer(List<ServerStreamTracer.Factory> streamTracerFactories) {
    return NettyServerBuilder
        .forPort(0)
        .flowControlWindow(65 * 1024)
        .setTransportTracerFactory(fakeClockTransportTracer)
        .buildTransportServer(streamTracerFactories);
  }

  @Override
  protected InternalServer newServer(
      InternalServer server, List<ServerStreamTracer.Factory> streamTracerFactories) {
    int port = server.getPort();
    return NettyServerBuilder
        .forPort(port)
        .flowControlWindow(65 * 1024)
        .setTransportTracerFactory(fakeClockTransportTracer)
        .buildTransportServer(streamTracerFactories);
  }

  @Override
  protected String testAuthority(InternalServer server) {
    return "localhost:" + server.getPort();
  }

  @Override
  protected void advanceClock(long offset, TimeUnit unit) {
    fakeClock.forwardNanos(unit.toNanos(offset));
  }

  @Override
  protected long fakeCurrentTimeNanos() {
    return fakeClock.getTicker().read();
  }

  @Override
  protected ManagedClientTransport newClientTransport(InternalServer server) {
    int port = server.getPort();
    return clientFactory.newClientTransport(
        new InetSocketAddress("localhost", port),
        new ClientTransportFactory.ClientTransportOptions()
          .setAuthority(testAuthority(server)));
  }
}
