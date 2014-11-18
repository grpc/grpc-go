package com.google.net.stubby.transport.netty;

import static com.google.net.stubby.transport.netty.NettyTestUtil.messageFrame;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyInt;
import static org.mockito.Matchers.anyLong;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.util.concurrent.SettableFuture;
import com.google.net.stubby.transport.AbstractStream;
import com.google.net.stubby.transport.StreamListener;

import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelPipeline;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;

import org.junit.Before;
import org.junit.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;

/**
 * Base class for Netty stream unit tests.
 */
public abstract class NettyStreamTestBase {
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
  protected Runnable accepted;

  @Mock
  protected EventLoop eventLoop;

  @Mock
  protected ChannelPromise promise;

  protected SettableFuture<Void> processingFuture;

  protected InputStream input;

  protected AbstractStream<Integer> stream;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);

    mockChannelFuture(true);
    when(channel.write(any())).thenReturn(future);
    when(channel.writeAndFlush(any())).thenReturn(future);
    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(channel.pipeline()).thenReturn(pipeline);
    when(channel.eventLoop()).thenReturn(eventLoop);
    when(pipeline.firstContext()).thenReturn(ctx);
    when(eventLoop.inEventLoop()).thenReturn(true);

    processingFuture = SettableFuture.create();
    when(listener().messageRead(any(InputStream.class), anyInt())).thenReturn(processingFuture);

    doAnswer(new Answer<Void>() {
      @Override
      public Void answer(InvocationOnMock invocation) throws Throwable {
        Runnable runnable = (Runnable) invocation.getArguments()[0];
        runnable.run();
        return null;
      }
    }).when(eventLoop).execute(any(Runnable.class));

    input = new ByteArrayInputStream(MESSAGE.getBytes(UTF_8));
    stream = createStream();
  }

  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    if (stream instanceof NettyServerStream) {
      ((NettyServerStream) stream).inboundDataReceived(messageFrame(MESSAGE), false);
    } else {
      ((NettyClientStream) stream).transportDataReceived(messageFrame(MESSAGE), false);
    }
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener()).messageRead(captor.capture(), eq(MESSAGE.length()));

    // Verify that inbound flow control window update has been disabled for the stream.
    assertEquals(MESSAGE, NettyTestUtil.toString(captor.getValue()));

    // Verify that inbound flow control window update has been re-enabled for the stream after
    // the future completes.
    processingFuture.set(null);
  }

  protected abstract AbstractStream<Integer> createStream();

  protected abstract StreamListener listener();

  private void mockChannelFuture(boolean succeeded) {
    when(future.isDone()).thenReturn(true);
    when(future.isCancelled()).thenReturn(false);
    when(future.isSuccess()).thenReturn(succeeded);
    when(future.awaitUninterruptibly(anyLong(), any(TimeUnit.class))).thenReturn(true);
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
