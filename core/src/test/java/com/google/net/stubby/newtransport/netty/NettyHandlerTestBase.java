package com.google.net.stubby.newtransport.netty;

import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.Mockito;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import io.netty.buffer.ByteBuf;
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
    ChannelHandlerContext ctx = newContext();
    frameWriter.writeGoAway(ctx, lastStreamId, 0, Unpooled.EMPTY_BUFFER, newPromise());
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
    return ctx;
  }

  protected final ChannelPromise newPromise() {
    return Mockito.mock(ChannelPromise.class);
  }

  protected final ByteBuf captureWrite(ChannelHandlerContext ctx) {
    ArgumentCaptor<ByteBuf> captor = ArgumentCaptor.forClass(ByteBuf.class);
    verify(ctx).write(captor.capture(), any(ChannelPromise.class));
    return captor.getValue();
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
    when(channel.eventLoop()).thenReturn(eventLoop);

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
