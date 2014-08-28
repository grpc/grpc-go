package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.GrpcFramingUtil.CONTEXT_VALUE_FRAME;
import static com.google.net.stubby.GrpcFramingUtil.PAYLOAD_FRAME;
import static com.google.net.stubby.GrpcFramingUtil.STATUS_FRAME;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.anyLong;
import static org.mockito.Matchers.eq;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.common.io.ByteStreams;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.transport.Transport.ContextValue;
import com.google.protobuf.ByteString;

import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;

import org.junit.Test;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;

/**
 * Base class for Netty stream unit tests.
 */
public abstract class NettyStreamTestBase {
  protected static final String CONTEXT_KEY = "key";
  protected static final String MESSAGE = "hello world";

  @Mock protected Channel channel;

  @Mock protected ChannelFuture future;

  @Mock protected StreamListener listener;

  @Mock protected Runnable accepted;

  @Mock protected EventLoop eventLoop;

  @Mock protected ChannelPromise promise;

  protected InputStream input;

  /**
   * Returns the NettyStream object to be tested.
   */
  protected abstract NettyStream stream();

  protected final void init() {
    MockitoAnnotations.initMocks(this);

    mockChannelFuture(true);
    when(channel.write(any())).thenReturn(future);
    when(channel.writeAndFlush(any())).thenReturn(future);
    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(channel.eventLoop()).thenReturn(eventLoop);
    when(eventLoop.inEventLoop()).thenReturn(true);

    input = new ByteArrayInputStream(MESSAGE.getBytes(UTF_8));
  }

  @Test
  public void inboundContextShouldCallListener() throws Exception {
    stream().inboundDataReceived(contextFrame(), false, promise);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).contextRead(eq(CONTEXT_KEY), captor.capture(), eq(MESSAGE.length()));
    verify(promise).setSuccess();
    assertEquals(MESSAGE, toString(captor.getValue()));
  }

  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    stream().inboundDataReceived(messageFrame(), false, promise);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).messageRead(captor.capture(), eq(MESSAGE.length()));
    verify(promise).setSuccess();
    assertEquals(MESSAGE, toString(captor.getValue()));
  }

  private String toString(InputStream in) throws Exception {
    byte[] bytes = new byte[in.available()];
    ByteStreams.readFully(in, bytes);
    return new String(bytes, UTF_8);
  }

  protected final ByteBuf contextFrame() throws Exception {
    byte[] body = ContextValue
        .newBuilder()
        .setKey(CONTEXT_KEY)
        .setValue(ByteString.copyFromUtf8(MESSAGE))
        .build()
        .toByteArray();
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    dos.write(CONTEXT_VALUE_FRAME);
    dos.writeInt(body.length);
    dos.write(body);
    dos.close();

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  protected final ByteBuf messageFrame() throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    dos.write(PAYLOAD_FRAME);
    dos.writeInt(MESSAGE.length());
    dos.write(MESSAGE.getBytes(UTF_8));
    dos.close();

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  protected final ByteBuf statusFrame(Status status) throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    short code = (short) status.getCode().getNumber();
    dos.write(STATUS_FRAME);
    int length = 2;
    dos.writeInt(length);
    dos.writeShort(code);

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  protected final ByteBuf compressionFrame(byte[] data) {
    ByteBuf buf = Unpooled.buffer();
    buf.writeInt(data.length);
    buf.writeBytes(data);
    return buf;
  }

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
