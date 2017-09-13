/*
 * Copyright 2016, gRPC Authors All rights reserved.
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

import static com.google.common.truth.Truth.assertThat;

import io.grpc.ServerStreamTracer;
import io.grpc.internal.ServerListener;
import io.grpc.internal.ServerTransport;
import io.grpc.internal.ServerTransportListener;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import java.net.InetSocketAddress;
import java.util.Collections;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

@RunWith(JUnit4.class)
public class NettyServerTest {

  @Test
  public void getPort() throws Exception {
    InetSocketAddress addr = new InetSocketAddress(0);
    NettyServer ns = new NettyServer(
        addr,
        NioServerSocketChannel.class,
        null, // no boss group
        null, // no event group
        new ProtocolNegotiators.PlaintextNegotiator(),
        null, // no channel init
        Collections.<ServerStreamTracer.Factory>emptyList(),
        1, // ignore
        1, // ignore
        1, // ignore
        1, // ignore
        1, // ignore
        1, 1, // ignore
        1, 1, // ignore
        true, 0); // ignore
    ns.start(new ServerListener() {
      @Override
      public ServerTransportListener transportCreated(ServerTransport transport) {
        return null;
      }

      @Override
      public void serverShutdown() {}
    });

    // Check that we got an actual port.
    assertThat(ns.getPort()).isGreaterThan(0);

    // Cleanup
    ns.shutdown();
  }

  @Test
  public void getPort_notStarted() throws Exception {
    InetSocketAddress addr = new InetSocketAddress(0);
    NettyServer ns = new NettyServer(
        addr,
        NioServerSocketChannel.class,
        null, // no boss group
        null, // no event group
        new ProtocolNegotiators.PlaintextNegotiator(),
        null, // no channel init
        Collections.<ServerStreamTracer.Factory>emptyList(),
        1, // ignore
        1, // ignore
        1, // ignore
        1, // ignore
        1, // ignore
        1, 1, // ignore
        1, 1, // ignore
        true, 0); // ignore

    assertThat(ns.getPort()).isEqualTo(-1);
  }
}
