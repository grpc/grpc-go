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

import static io.grpc.netty.NettyTestUtil.messageFrame;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyLong;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.reset;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.internal.AbstractStream;
import io.grpc.internal.StreamListener;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelPromise;
import io.netty.channel.EventLoop;
import io.netty.handler.codec.http2.Http2Stream;

import org.junit.Before;
import org.junit.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;

/**
 * Base class for Netty stream unit tests.
 */
public abstract class NettyStreamTestBase<T extends AbstractStream<Integer>> {
  protected static final String MESSAGE = "hello world";
  protected static final int STREAM_ID = 1;

  @Mock
  protected Channel channel;

  @Mock
  private ChannelHandlerContext ctx;

  @Mock
  private ChannelPipeline pipeline;

  @Mock
  protected ChannelFuture future;

  @Mock
  protected EventLoop eventLoop;

  @Mock
  protected ChannelPromise promise;

  @Mock
  protected Http2Stream http2Stream;

  @Mock
  protected WriteQueue writeQueue;

  protected T stream;

  /** Set up for test. */
  @Before
  public void setUp() {
    MockitoAnnotations.initMocks(this);

    mockFuture(true);
    when(channel.write(any())).thenReturn(future);
    when(channel.writeAndFlush(any())).thenReturn(future);
    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(channel.pipeline()).thenReturn(pipeline);
    when(channel.eventLoop()).thenReturn(eventLoop);
    when(channel.newPromise()).thenReturn(new DefaultChannelPromise(channel));
    when(channel.voidPromise()).thenReturn(new DefaultChannelPromise(channel));
    when(pipeline.firstContext()).thenReturn(ctx);
    when(eventLoop.inEventLoop()).thenReturn(true);
    when(http2Stream.id()).thenReturn(STREAM_ID);

    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        Runnable runnable = (Runnable) invocation.getArguments()[0];
        runnable.run();
        return null;
      }
    }).when(eventLoop).execute(any(Runnable.class));

    stream = createStream();
  }

  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    stream.request(1);

    if (stream instanceof NettyServerStream) {
      ((NettyServerStream) stream).inboundDataReceived(messageFrame(MESSAGE), false);
    } else {
      ((NettyClientStream) stream).transportDataReceived(messageFrame(MESSAGE), false);
    }
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener()).messageRead(captor.capture());

    // Verify that inbound flow control window update has been disabled for the stream.
    assertEquals(MESSAGE, NettyTestUtil.toString(captor.getValue()));
  }

  @Test
  public void shouldBeImmediatelyReadyForData() {
    assertTrue(stream.isReady());
  }

  @Test
  public void closedShouldNotBeReady() throws IOException {
    assertTrue(stream.isReady());
    closeStream();
    assertFalse(stream.isReady());
  }

  @Test
  public void notifiedOnReadyAfterWriteCompletes() throws IOException {
    sendHeadersIfServer();
    assertTrue(stream.isReady());
    byte[] msg = largeMessage();
    // The future is set up to automatically complete, indicating that the write is done.
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    assertTrue(stream.isReady());
    verify(listener()).onReady();
  }

  @Test
  public void shouldBeReadyForDataAfterWritingSmallMessage() throws IOException {
    sendHeadersIfServer();
    // Make sure the writes don't complete so we "back up"
    reset(future);

    assertTrue(stream.isReady());
    byte[] msg = smallMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    assertTrue(stream.isReady());
    verify(listener(), never()).onReady();
  }

  @Test
  public void shouldNotBeReadyForDataAfterWritingLargeMessage() throws IOException {
    sendHeadersIfServer();
    // Make sure the writes don't complete so we "back up"
    reset(future);

    assertTrue(stream.isReady());
    byte[] msg = largeMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    assertFalse(stream.isReady());
    verify(listener(), never()).onReady();
  }

  protected byte[] smallMessage() {
    return MESSAGE.getBytes();
  }

  protected byte[] largeMessage() {
    byte[] smallMessage = smallMessage();
    int size = smallMessage.length * 10 * 1024;
    byte[] largeMessage = new byte[size];
    for (int ix = 0; ix < size; ix += smallMessage.length) {
      System.arraycopy(smallMessage, 0, largeMessage, ix, smallMessage.length);
    }
    return largeMessage;
  }

  protected abstract T createStream();

  protected abstract void sendHeadersIfServer();

  protected abstract StreamListener listener();

  protected abstract void closeStream();

  private void mockFuture(boolean succeeded) {
    when(future.isDone()).thenReturn(true);
    when(future.isCancelled()).thenReturn(false);
    when(future.isSuccess()).thenReturn(succeeded);
    when(future.awaitUninterruptibly(anyLong(), any(TimeUnit.class))).thenReturn(true);
    if (!succeeded) {
      when(future.cause()).thenReturn(new Exception("fake"));
    }
  }
}
