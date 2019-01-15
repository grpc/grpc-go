/*
 * Copyright 2019 The gRPC Authors
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
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.junit.Assert.fail;

import io.grpc.Status;
import io.grpc.Status.Code;
import io.netty.bootstrap.Bootstrap;
import io.netty.bootstrap.ServerBootstrap;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerAdapter;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelOutboundHandlerAdapter;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultEventLoop;
import io.netty.channel.EventLoop;
import io.netty.channel.local.LocalAddress;
import io.netty.channel.local.LocalChannel;
import io.netty.channel.local.LocalServerChannel;
import java.net.ConnectException;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicReference;
import org.junit.After;
import org.junit.Rule;
import org.junit.Test;
import org.junit.rules.DisableOnDebug;
import org.junit.rules.TestRule;
import org.junit.rules.Timeout;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;

/**
 * Tests for {@link WriteBufferingAndExceptionHandler}.
 */
@RunWith(JUnit4.class)
public class WriteBufferingAndExceptionHandlerTest {

  private static final long TIMEOUT_SECONDS = 10;

  @Rule
  public final TestRule timeout = new DisableOnDebug(Timeout.seconds(TIMEOUT_SECONDS));

  private final EventLoop group = new DefaultEventLoop();
  private Channel chan;
  private Channel server;

  @After
  public void tearDown() {
    if (server != null) {
      server.close();
    }
    if (chan != null) {
      chan.close();
    }
    group.shutdownGracefully();
  }

  @Test
  public void connectionFailuresPropagated() throws Exception {
    WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelHandlerAdapter() {});
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    // Write before connect.  In the event connect fails, the pipeline is torn down and the handler
    // won't be able to fail the writes with the correct exception.
    ChannelFuture wf = chan.writeAndFlush(new Object());
    chan.connect(new LocalAddress("bogus"));

    try {
      wf.sync();
      fail();
    } catch (Exception e) {
      assertThat(e).isInstanceOf(ConnectException.class);
      assertThat(e).hasMessageThat().contains("connection refused");
    }
  }

  @Test
  public void channelInactiveFailuresPropagated() throws Exception {
    WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelHandlerAdapter() {});
    LocalAddress addr = new LocalAddress("local");
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    ChannelFuture sf = new ServerBootstrap()
        .channel(LocalServerChannel.class)
        .childHandler(new ChannelHandlerAdapter() {})
        .group(group)
        .bind(addr);
    server = sf.channel();
    sf.sync();

    ChannelFuture wf = chan.writeAndFlush(new Object());
    chan.connect(addr);
    chan.pipeline().fireChannelInactive();

    try {
      wf.sync();
      fail();
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertThat(status.getCode()).isEqualTo(Code.UNAVAILABLE);
      assertThat(status.getDescription())
          .contains("Connection closed while performing protocol negotiation");
    }
  }

  @Test
  public void channelCloseFailuresPropagated() throws Exception {
    WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelHandlerAdapter() {});
    LocalAddress addr = new LocalAddress("local");
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    ChannelFuture sf = new ServerBootstrap()
        .channel(LocalServerChannel.class)
        .childHandler(new ChannelHandlerAdapter() {})
        .group(group)
        .bind(addr);
    server = sf.channel();
    sf.sync();

    ChannelFuture wf = chan.writeAndFlush(new Object());
    chan.connect(addr);
    chan.close();

    try {
      wf.sync();
      fail();
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertThat(status.getCode()).isEqualTo(Code.UNAVAILABLE);
      assertThat(status.getDescription())
          .contains("Connection closing while performing protocol negotiation");
    }
  }

  @Test
  public void uncaughtExceptionFailuresPropagated() throws Exception {
    WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelHandlerAdapter() {});
    LocalAddress addr = new LocalAddress("local");
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    ChannelFuture sf = new ServerBootstrap()
        .channel(LocalServerChannel.class)
        .childHandler(new ChannelHandlerAdapter() {})
        .group(group)
        .bind(addr);
    server = sf.channel();
    sf.sync();

    ChannelFuture wf = chan.writeAndFlush(new Object());
    chan.connect(addr);
    chan.pipeline().fireExceptionCaught(Status.ABORTED.withDescription("zap").asRuntimeException());

    try {
      wf.sync();
      fail();
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertThat(status.getCode()).isEqualTo(Code.ABORTED);
      assertThat(status.getDescription()).contains("zap");
    }
  }

  @Test
  public void handlerRemovedFailuresPropagated() throws Exception {
    WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelHandlerAdapter() {
          @Override
          public void handlerRemoved(ChannelHandlerContext ctx) {
            ctx.pipeline().remove(
                ctx.pipeline().context(WriteBufferingAndExceptionHandler.class).name());
          }
        });
    LocalAddress addr = new LocalAddress("local");
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    ChannelFuture sf = new ServerBootstrap()
        .channel(LocalServerChannel.class)
        .childHandler(new ChannelHandlerAdapter() {})
        .group(group)
        .bind(addr);
    server = sf.channel();
    sf.sync();

    chan.connect(addr);
    ChannelFuture wf = chan.writeAndFlush(new Object());
    chan.pipeline().removeFirst();

    try {
      wf.sync();
      fail();
    } catch (Exception e) {
      Status status = Status.fromThrowable(e);
      assertThat(status.getCode()).isEqualTo(Code.INTERNAL);
      assertThat(status.getDescription()).contains("Buffer removed");
    }
  }

  @Test
  public void writesBuffered() throws Exception {
    final AtomicBoolean handlerAdded = new AtomicBoolean();
    final AtomicBoolean flush = new AtomicBoolean();
    final AtomicReference<Object> write = new AtomicReference<>();
    final WriteBufferingAndExceptionHandler handler =
        new WriteBufferingAndExceptionHandler(new ChannelOutboundHandlerAdapter() {
          @Override
          public void handlerAdded(ChannelHandlerContext ctx) throws Exception {
            assertFalse(handlerAdded.getAndSet(true));
            super.handlerAdded(ctx);
          }

          @Override
          public void flush(ChannelHandlerContext ctx) throws Exception {
            assertFalse(flush.getAndSet(true));
            super.flush(ctx);
          }

          @Override
          public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) {
            assertNull(write.getAndSet(msg));
            promise.setSuccess();
          }
        });
    LocalAddress addr = new LocalAddress("local");
    ChannelFuture cf = new Bootstrap()
        .channel(LocalChannel.class)
        .handler(handler)
        .group(group)
        .register();
    chan = cf.channel();
    cf.sync();
    ChannelFuture sf = new ServerBootstrap()
        .channel(LocalServerChannel.class)
        .childHandler(new ChannelHandlerAdapter() {})
        .group(group)
        .bind(addr);
    server = sf.channel();
    sf.sync();

    assertTrue(handlerAdded.get());

    chan.write(new Object());
    chan.connect(addr).sync();
    assertNull(write.get());

    chan.flush();
    assertNull(write.get());
    assertFalse(flush.get());

    assertThat(chan.pipeline().context(handler)).isNotNull();
    chan.eventLoop().submit(new Runnable() {
      @Override
      public void run() {
        handler.writeBufferedAndRemove(chan.pipeline().context(handler));
      }
    }).sync();

    assertThat(chan.pipeline().context(handler)).isNull();
    assertThat(write.get().getClass()).isSameAs(Object.class);
    assertTrue(flush.get());
    assertThat(chan.pipeline()).doesNotContain(handler);
  }
}
