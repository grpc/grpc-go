/*
 * Copyright 2014, Google Inc. All rights reserved.
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

package io.grpc.transport.netty;

import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.CompositeByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;
import io.netty.handler.codec.http2.Http2FrameListener;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;

import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Base class for Netty handler unit tests.
 */
@RunWith(JUnit4.class)
public abstract class NettyHandlerTestBase {

  @Mock
  protected Channel channel;

  @Mock
  protected ChannelHandlerContext ctx;

  @Mock
  protected ChannelFuture future;

  @Mock
  protected ChannelPromise promise;

  @Mock
  protected ChannelFuture succeededFuture;

  @Mock
  protected Http2FrameListener frameListener;

  @Mock
  protected EventLoop eventLoop;

  protected Http2FrameWriter frameWriter;
  protected Http2FrameReader frameReader;

  protected final ByteBuf headersFrame(int streamId, Http2Headers headers) {
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeHeaders(ctx, streamId, headers, 0, false, promise);
    return captureWrite(ctx);
  }

  protected final ByteBuf goAwayFrame(int lastStreamId) {
    return goAwayFrame(lastStreamId, 0, Unpooled.EMPTY_BUFFER);
  }

  protected final ByteBuf goAwayFrame(int lastStreamId, int errorCode, ByteBuf data) {
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeGoAway(ctx, lastStreamId, errorCode, data, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf rstStreamFrame(int streamId, int errorCode) {
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeRstStream(ctx, streamId, errorCode, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf serializeSettings(Http2Settings settings) {
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeSettings(ctx, settings, newPromise());
    return captureWrite(ctx);
  }

  protected final ChannelHandlerContext newContext() {
    ChannelHandlerContext ctx = Mockito.mock(ChannelHandlerContext.class);
    when(ctx.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(ctx.executor()).thenReturn(eventLoop);
    return ctx;
  }

  protected final ChannelPromise newPromise() {
    return Mockito.mock(ChannelPromise.class);
  }

  protected final ByteBuf captureWrite(ChannelHandlerContext ctx) {
    ArgumentCaptor<ByteBuf> captor = ArgumentCaptor.forClass(ByteBuf.class);
    verify(ctx, atLeastOnce()).write(captor.capture(), any(ChannelPromise.class));
    CompositeByteBuf composite = Unpooled.compositeBuffer();
    for (ByteBuf buf : captor.getAllValues()) {
      composite.addComponent(buf);
      composite.writerIndex(composite.writerIndex() + buf.readableBytes());
    }
    return composite;
  }

  protected final void mockContext() {
    Mockito.reset(ctx);
    Mockito.reset(promise);
    when(ctx.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(ctx.channel()).thenReturn(channel);
    when(ctx.write(any())).thenReturn(future);
    when(ctx.write(any(), eq(promise))).thenReturn(future);
    when(ctx.writeAndFlush(any())).thenReturn(future);
    when(ctx.writeAndFlush(any(), eq(promise))).thenReturn(future);
    when(ctx.newPromise()).thenReturn(promise);
    when(ctx.newSucceededFuture()).thenReturn(succeededFuture);
    when(ctx.executor()).thenReturn(eventLoop);
    when(channel.eventLoop()).thenReturn(eventLoop);

    mockFuture(succeededFuture, true);

    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        Runnable runnable = (Runnable) invocation.getArguments()[0];
        runnable.run();
        return null;
      }
    }).when(eventLoop).execute(any(Runnable.class));
  }

  protected final void mockFuture(boolean succeeded) {
    mockFuture(future, succeeded);
  }

  protected final void mockFuture(final ChannelFuture future, boolean succeeded) {
    when(future.isDone()).thenReturn(true);
    when(future.isCancelled()).thenReturn(false);
    when(future.isSuccess()).thenReturn(succeeded);
    if (!succeeded) {
      when(future.cause()).thenReturn(new Exception("fake"));
    }

    doAnswer(new Answer<ChannelFuture>() {
      @Override
      public ChannelFuture answer(InvocationOnMock invocation) throws Throwable {
        ChannelFutureListener listener = (ChannelFutureListener) invocation.getArguments()[0];
        listener.operationComplete(future);
        return future;
      }
    }).when(future).addListener(any(ChannelFutureListener.class));
  }
}
