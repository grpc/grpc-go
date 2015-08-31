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

package io.grpc.netty;

import static com.google.common.base.Charsets.UTF_8;
import static org.mockito.Matchers.any;
import static org.mockito.Mockito.atLeastOnce;
import static org.mockito.Mockito.spy;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.ByteBufUtil;
import io.netty.buffer.CompositeByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.handler.codec.http2.DefaultHttp2FrameReader;
import io.netty.handler.codec.http2.DefaultHttp2FrameWriter;
import io.netty.handler.codec.http2.Http2Connection;
import io.netty.handler.codec.http2.Http2ConnectionHandler;
import io.netty.handler.codec.http2.Http2Exception;
import io.netty.handler.codec.http2.Http2FrameReader;
import io.netty.handler.codec.http2.Http2FrameWriter;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.handler.codec.http2.Http2Settings;

import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mockito;
import org.mockito.verification.VerificationMode;

/**
 * Base class for Netty handler unit tests.
 */
@RunWith(JUnit4.class)
public abstract class NettyHandlerTestBase<T extends Http2ConnectionHandler> {

  private ByteBuf content;

  private EmbeddedChannel channel;

  private ChannelHandlerContext ctx;

  private Http2FrameWriter frameWriter;

  private Http2FrameReader frameReader;

  private T handler;

  private WriteQueue writeQueue;

  /**
   * Must be called by subclasses to initialize the handler and channel.
   */
  protected final void initChannel() throws Exception {
    content = Unpooled.copiedBuffer("hello world", UTF_8);
    frameWriter = spy(new DefaultHttp2FrameWriter());
    frameReader = new DefaultHttp2FrameReader();

    handler = newHandler();

    channel = new EmbeddedChannel(handler);
    ctx = channel.pipeline().context(handler);

    writeQueue = initWriteQueue();
  }

  protected final T handler() {
    return handler;
  }

  protected final EmbeddedChannel channel() {
    return channel;
  }

  protected final ChannelHandlerContext ctx() {
    return ctx;
  }

  protected final Http2FrameWriter frameWriter() {
    return frameWriter;
  }

  protected final Http2FrameReader frameReader() {
    return frameReader;
  }

  protected final ByteBuf content() {
    return content;
  }

  protected final byte[] contentAsArray() {
    return ByteBufUtil.getBytes(content());
  }

  protected final Http2FrameWriter verifyWrite() {
    return verify(frameWriter);
  }

  protected final Http2FrameWriter verifyWrite(VerificationMode verificationMode) {
    return verify(frameWriter, verificationMode);
  }

  protected final void channelRead(Object obj) throws Exception {
    handler().channelRead(ctx, obj);
  }

  protected final ByteBuf dataFrame(int streamId, boolean endStream, ByteBuf content) {
    // Need to retain the content since the frameWriter releases it.
    content.retain();

    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writeData(ctx, streamId, content, 0, endStream, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf pingFrame(boolean ack, ByteBuf payload) {
    // Need to retain the content since the frameWriter releases it.
    payload.retain();

    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writePing(ctx, ack, payload, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf headersFrame(int streamId, Http2Headers headers) {
    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writeHeaders(ctx, streamId, headers, 0, false, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf goAwayFrame(int lastStreamId) {
    return goAwayFrame(lastStreamId, 0, Unpooled.EMPTY_BUFFER);
  }

  protected final ByteBuf goAwayFrame(int lastStreamId, int errorCode, ByteBuf data) {
    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writeGoAway(ctx, lastStreamId, errorCode, data, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf rstStreamFrame(int streamId, int errorCode) {
    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writeRstStream(ctx, streamId, errorCode, newPromise());
    return captureWrite(ctx);
  }

  protected final ByteBuf serializeSettings(Http2Settings settings) {
    ChannelHandlerContext ctx = newMockContext();
    new DefaultHttp2FrameWriter().writeSettings(ctx, settings, newPromise());
    return captureWrite(ctx);
  }

  protected final ChannelPromise newPromise() {
    return channel.newPromise();
  }

  protected final Http2Connection connection() {
    return handler().connection();
  }

  protected final ChannelFuture enqueue(Object command) {
    ChannelFuture future = writeQueue.enqueue(command, newPromise(), true);
    channel.runPendingTasks();
    return future;
  }

  protected final ChannelHandlerContext newMockContext() {
    ChannelHandlerContext ctx = Mockito.mock(ChannelHandlerContext.class);
    when(ctx.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    EventLoop eventLoop = Mockito.mock(EventLoop.class);
    when(ctx.executor()).thenReturn(eventLoop);
    return ctx;
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

  protected abstract T newHandler() throws Http2Exception;

  protected abstract WriteQueue initWriteQueue();
}
