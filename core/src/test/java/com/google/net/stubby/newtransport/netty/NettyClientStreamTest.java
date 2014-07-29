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
import com.google.net.stubby.newtransport.HttpUtil;
import com.google.net.stubby.newtransport.StreamListener;
import com.google.net.stubby.newtransport.StreamState;
import com.google.net.stubby.transport.Transport;
import com.google.net.stubby.transport.Transport.ContextValue;
import com.google.protobuf.ByteString;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;
import org.mockito.MockitoAnnotations;
import org.mockito.invocation.InvocationOnMock;
import org.mockito.stubbing.Answer;

import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;
import io.netty.buffer.ByteBuf;
import io.netty.buffer.Unpooled;
import io.netty.buffer.UnpooledByteBufAllocator;
import io.netty.channel.Channel;
import io.netty.channel.ChannelFuture;
import io.netty.channel.ChannelFutureListener;
import io.netty.channel.ChannelPromise;
import io.netty.channel.EventLoop;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.InputStream;
import java.util.concurrent.TimeUnit;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest {
  private static final String CONTEXT_KEY = "key";
  private static final String MESSAGE = "hello world";

  private NettyClientStream stream;

  @Mock
  private StreamListener listener;

  @Mock
  private Channel channel;

  @Mock
  private ChannelFuture future;

  @Mock
  private ChannelPromise promise;

  @Mock
  private EventLoop eventLoop;

  private InputStream input;

  @Mock
  private Runnable accepted;

  @Before
  public void setup() {
    MockitoAnnotations.initMocks(this);

    mockChannelFuture(true);
    when(channel.write(any())).thenReturn(future);
    when(channel.writeAndFlush(any())).thenReturn(future);
    when(channel.alloc()).thenReturn(UnpooledByteBufAllocator.DEFAULT);
    when(channel.eventLoop()).thenReturn(eventLoop);
    when(eventLoop.inEventLoop()).thenReturn(true);

    stream = new NettyClientStream(listener, channel);
    assertEquals(StreamState.OPEN, stream.state());
    input = new ByteArrayInputStream(MESSAGE.getBytes(UTF_8));
  }

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream.id(1);
    stream.halfClose();
    assertEquals(StreamState.READ_ONLY, stream.state());
  }

  @Test
  public void cancelShouldSendCommand() {
    stream.cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void writeContextShouldSendRequest() throws Exception {
    // Force stream creation.
    stream.id(1);
    stream.writeContext(CONTEXT_KEY, input, input.available(), accepted);
    stream.flush();
    ArgumentCaptor<SendGrpcFrameCommand> captor =
        ArgumentCaptor.forClass(SendGrpcFrameCommand.class);
    verify(channel).writeAndFlush(captor.capture());
    assertEquals(contextFrame(), captor.getValue().content());
    verify(accepted).run();
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream.id(1);
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    ArgumentCaptor<SendGrpcFrameCommand> captor =
        ArgumentCaptor.forClass(SendGrpcFrameCommand.class);
    verify(channel).writeAndFlush(captor.capture());
    assertEquals(messageFrame(), captor.getValue().content());
    verify(accepted).run();
  }

  @Test
  public void setStatusWithOkShouldCloseStream() {
    stream.id(1);
    stream.setStatus(Status.OK);
    verify(listener).closed(Status.OK);
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldCloseStream() {
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream.setStatus(errorStatus);
    verify(listener).closed(eq(errorStatus));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream.setStatus(errorStatus);
    stream.setStatus(Status.OK);
    verify(listener).closed(any(Status.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = new Status(Transport.Code.INTERNAL);
    stream.setStatus(errorStatus);
    stream.setStatus(Status.fromThrowable(new RuntimeException("fake")));
    verify(listener).closed(any(Status.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void inboundContextShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream.inboundHeadersRecieved(grpcResponseHeaders());

    stream.inboundDataReceived(contextFrame(), false, promise);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).contextRead(eq(CONTEXT_KEY), captor.capture(), eq(MESSAGE.length()));
    verify(promise).setSuccess();
    assertEquals(MESSAGE, toString(captor.getValue()));
  }

  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream.inboundHeadersRecieved(grpcResponseHeaders());

    stream.inboundDataReceived(messageFrame(), false, promise);
    ArgumentCaptor<InputStream> captor = ArgumentCaptor.forClass(InputStream.class);
    verify(listener).messageRead(captor.capture(), eq(MESSAGE.length()));
    verify(promise).setSuccess();
    assertEquals(MESSAGE, toString(captor.getValue()));
  }

  @Test
  public void inboundStatusShouldSetStatus() throws Exception {
    stream.id(1);

    // Receive headers first so that it's a valid GRPC response.
    stream.inboundHeadersRecieved(grpcResponseHeaders());

    stream.inboundDataReceived(statusFrame(), false, promise);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture());
    assertEquals(Transport.Code.INTERNAL, captor.getValue().getCode());
    verify(promise).setSuccess();
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream.inboundDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true, promise);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture());
    assertEquals(MESSAGE, captor.getValue().getDescription());
  }

  private String toString(InputStream in) throws Exception {
    byte[] bytes = new byte[in.available()];
    ByteStreams.readFully(in, bytes);
    return new String(bytes, UTF_8);
  }

  private ByteBuf contextFrame() throws Exception {
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

  private ByteBuf messageFrame() throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    dos.write(PAYLOAD_FRAME);
    dos.writeInt(MESSAGE.length());
    dos.write(MESSAGE.getBytes(UTF_8));
    dos.close();

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  private ByteBuf statusFrame() throws Exception {
    ByteArrayOutputStream os = new ByteArrayOutputStream();
    DataOutputStream dos = new DataOutputStream(os);
    short code = (short) Transport.Code.INTERNAL.getNumber();
    dos.write(STATUS_FRAME);
    int length = 2;
    dos.writeInt(length);
    dos.writeShort(code);

    // Write the compression header followed by the context frame.
    return compressionFrame(os.toByteArray());
  }

  private ByteBuf compressionFrame(byte[] data) {
    ByteBuf buf = Unpooled.buffer();
    buf.writeInt(data.length);
    buf.writeBytes(data);
    return buf;
  }

  private Http2Headers grpcResponseHeaders() {
    return DefaultHttp2Headers.newBuilder().status("200")
        .set(HttpUtil.CONTENT_TYPE_HEADER, HttpUtil.CONTENT_TYPE_PROTORPC).build();
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
