/*
 * Copyright 2014 The gRPC Authors
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

import static com.google.common.base.Charsets.US_ASCII;
import static io.grpc.netty.NettyTestUtil.messageFrame;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNull;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyBoolean;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import io.grpc.internal.Stream;
import io.grpc.internal.StreamListener;
import io.grpc.netty.WriteQueue.QueuedCommand;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.ChannelPromise;
import io.netty.channel.DefaultChannelPromise;
import io.netty.channel.EventLoop;
import io.netty.handler.codec.http2.Http2Stream;
import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.Queue;
import org.junit.Before;
import org.junit.Test;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

/**
 * Base class for Netty stream unit tests.
 */
public abstract class NettyStreamTestBase<T extends Stream> {
  protected static final String MESSAGE = "hello world";
  protected static final int STREAM_ID = 1;

  @Mock
  protected Channel channel;

  @Mock
  private ChannelHandlerContext ctx;

  @Mock
  private ChannelPipeline pipeline;

  @Mock
  protected EventLoop eventLoop;

  // ChannelPromise has too many methods to implement; we stubbed all necessary methods of Future.
  @SuppressWarnings("DoNotMock")
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

    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(channel.pipeline()).thenReturn(pipeline);
    when(channel.eventLoop()).thenReturn(eventLoop);
    when(channel.newPromise()).thenReturn(new DefaultChannelPromise(channel));
    when(channel.voidPromise()).thenReturn(new DefaultChannelPromise(channel));
    ChannelPromise completedPromise = new DefaultChannelPromise(channel)
        .setSuccess();
    when(channel.write(any())).thenReturn(completedPromise);
    when(channel.writeAndFlush(any())).thenReturn(completedPromise);
    when(writeQueue.enqueue(any(QueuedCommand.class), anyBoolean())).thenReturn(completedPromise);
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
      ((NettyServerStream) stream).transportState()
          .inboundDataReceived(messageFrame(MESSAGE), false);
    } else {
      ((NettyClientStream) stream).transportState()
          .transportDataReceived(messageFrame(MESSAGE), false);
    }

    InputStream message = listenerMessageQueue().poll();

    // Verify that inbound flow control window update has been disabled for the stream.
    assertEquals(MESSAGE, NettyTestUtil.toString(message));
    assertNull("no additional message expected", listenerMessageQueue().poll());
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
    // The channel.write future is set up to automatically complete, indicating that the write is
    // done.
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    assertTrue(stream.isReady());
    verify(listener()).onReady();
  }

  @Test
  public void shouldBeReadyForDataAfterWritingSmallMessage() throws IOException {
    sendHeadersIfServer();
    // Make sure the writes don't complete so we "back up"
    ChannelPromise uncompletedPromise = new DefaultChannelPromise(channel);
    when(writeQueue.enqueue(any(QueuedCommand.class), anyBoolean())).thenReturn(uncompletedPromise);

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
    ChannelPromise uncompletedPromise = new DefaultChannelPromise(channel);
    when(writeQueue.enqueue(any(QueuedCommand.class), anyBoolean())).thenReturn(uncompletedPromise);

    assertTrue(stream.isReady());
    byte[] msg = largeMessage();
    stream.writeMessage(new ByteArrayInputStream(msg));
    stream.flush();
    assertFalse(stream.isReady());
    verify(listener(), never()).onReady();
  }

  protected byte[] smallMessage() {
    return MESSAGE.getBytes(US_ASCII);
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

  protected abstract Queue<InputStream> listenerMessageQueue();

  protected abstract void closeStream();
}
