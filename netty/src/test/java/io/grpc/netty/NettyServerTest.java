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
import io.grpc.internal.TransportTracer;
import io.netty.channel.Channel;
import io.netty.channel.ChannelOption;
import io.netty.channel.WriteBufferWaterMark;
import io.netty.channel.socket.nio.NioServerSocketChannel;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.atomic.AtomicInteger;

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
        new HashMap<ChannelOption<?>, Object>(),
        null, // no boss group
        null, // no event group
        new ProtocolNegotiators.PlaintextNegotiator(),
        Collections.<ServerStreamTracer.Factory>emptyList(),
        TransportTracer.getDefaultFactory(),
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
        new HashMap<ChannelOption<?>, Object>(),
        null, // no boss group
        null, // no event group
        new ProtocolNegotiators.PlaintextNegotiator(),
        Collections.<ServerStreamTracer.Factory>emptyList(),
        TransportTracer.getDefaultFactory(),
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

  @Test(timeout = 60000)
  public void childChannelOptions() throws Exception {
    final int originalLowWaterMark = 2097169;
    final int originalHighWaterMark = 2097211;

    Map<ChannelOption<?>, Object> channelOptions = new HashMap<ChannelOption<?>, Object>();

    channelOptions.put(ChannelOption.WRITE_BUFFER_WATER_MARK,
        new WriteBufferWaterMark(originalLowWaterMark, originalHighWaterMark));

    final AtomicInteger lowWaterMark = new AtomicInteger(0);
    final AtomicInteger highWaterMark = new AtomicInteger(0);

    final CountDownLatch countDownLatch = new CountDownLatch(1);

    InetSocketAddress addr = new InetSocketAddress(0);
    NettyServer ns = new NettyServer(
        addr,
        NioServerSocketChannel.class,
        channelOptions,
        null, // no boss group
        null, // no event group
        new ProtocolNegotiators.PlaintextNegotiator(),
        Collections.<ServerStreamTracer.Factory>emptyList(),
        TransportTracer.getDefaultFactory(),
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
        Channel channel = ((NettyServerTransport)transport).channel();
        WriteBufferWaterMark writeBufferWaterMark = channel.config()
            .getOption(ChannelOption.WRITE_BUFFER_WATER_MARK);
        lowWaterMark.set(writeBufferWaterMark.low());
        highWaterMark.set(writeBufferWaterMark.high());

        countDownLatch.countDown();

        return null;
      }

      @Override
      public void serverShutdown() {}
    });

    Socket socket = new Socket();
    socket.connect(new InetSocketAddress("localhost", ns.getPort()), /* timeout= */ 8000);
    countDownLatch.await();
    socket.close();

    assertThat(lowWaterMark.get()).isEqualTo(originalLowWaterMark);
    assertThat(highWaterMark.get()).isEqualTo(originalHighWaterMark);

    ns.shutdown();
  }
}
