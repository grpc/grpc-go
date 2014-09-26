package com.google.net.stubby.newtransport.netty;

import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_HEADER;
import static com.google.net.stubby.newtransport.netty.Utils.CONTENT_TYPE_PROTORPC;
import static com.google.net.stubby.newtransport.netty.Utils.STATUS_OK;
import static io.netty.util.CharsetUtil.UTF_8;
import static org.junit.Assert.assertEquals;
import static org.mockito.Matchers.any;
import static org.mockito.Matchers.eq;
import static org.mockito.Matchers.same;
import static org.mockito.Mockito.verify;

import com.google.net.stubby.Metadata;
import com.google.net.stubby.Status;
import com.google.net.stubby.newtransport.ClientStreamListener;
import com.google.net.stubby.newtransport.StreamState;

import io.netty.buffer.Unpooled;
import io.netty.handler.codec.http2.DefaultHttp2Headers;
import io.netty.handler.codec.http2.Http2Headers;

import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.JUnit4;
import org.mockito.ArgumentCaptor;
import org.mockito.Mock;

/**
 * Tests for {@link NettyClientStream}.
 */
@RunWith(JUnit4.class)
public class NettyClientStreamTest extends NettyStreamTestBase {
  @Mock
  protected ClientStreamListener listener;

  @Override
  protected ClientStreamListener listener() {
    return listener;
  }

  @Test
  public void closeShouldSucceed() {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream().halfClose();
    assertEquals(StreamState.READ_ONLY, stream.state());
  }

  @Test
  public void cancelShouldSendCommand() {
    stream().cancel();
    verify(channel).writeAndFlush(any(CancelStreamCommand.class));
  }

  @Test
  public void writeMessageShouldSendRequest() throws Exception {
    // Force stream creation.
    stream().id(STREAM_ID);
    stream.writeMessage(input, input.available(), accepted);
    stream.flush();
    verify(channel).writeAndFlush(new SendGrpcFrameCommand(1, messageFrame(), false));
    verify(accepted).run();
  }

  @Test
  public void setStatusWithOkShouldCloseStream() {
    stream().id(1);
    stream().setStatus(Status.OK, new Metadata.Trailers());
    verify(listener).closed(same(Status.OK), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldCloseStream() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    verify(listener).closed(eq(errorStatus), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithOkShouldNotOverrideError() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.OK, new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void setStatusWithErrorShouldNotOverridePreviousError() {
    Status errorStatus = Status.INTERNAL;
    stream().setStatus(errorStatus, new Metadata.Trailers());
    stream().setStatus(Status.fromThrowable(new RuntimeException("fake")),
        new Metadata.Trailers());
    verify(listener).closed(any(Status.class), any(Metadata.Trailers.class));
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Override
  @Test
  public void inboundMessageShouldCallListener() throws Exception {
    // Receive headers first so that it's a valid GRPC response.
    stream().id(1);
    stream().inboundHeadersRecieved(grpcResponseHeaders(), false);

    super.inboundMessageShouldCallListener();
  }

  @Test
  public void inboundStatusShouldSetStatus() throws Exception {
    stream().id(1);

    // Receive headers first so that it's a valid GRPC response.
    stream().inboundHeadersRecieved(grpcResponseHeaders(), false);

    stream.inboundDataReceived(statusFrame(Status.INTERNAL), false);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(Status.INTERNAL.getCode(), captor.getValue().getCode());
    assertEquals(StreamState.CLOSED, stream.state());
  }

  @Test
  public void nonGrpcResponseShouldSetStatus() throws Exception {
    stream.inboundDataReceived(Unpooled.copiedBuffer(MESSAGE, UTF_8), true);
    ArgumentCaptor<Status> captor = ArgumentCaptor.forClass(Status.class);
    verify(listener).closed(captor.capture(), any(Metadata.Trailers.class));
    assertEquals(MESSAGE, captor.getValue().getDescription());
  }

  @Override
  protected NettyStream createStream() {
    NettyStream stream = new NettyClientStream(listener, channel, inboundFlow);
    assertEquals(StreamState.OPEN, stream.state());
    return stream;
  }

  private NettyClientStream stream() {
    return (NettyClientStream) stream;
  }

  private Http2Headers grpcResponseHeaders() {
    return new DefaultHttp2Headers()
        .status(STATUS_OK)
        .set(CONTENT_TYPE_HEADER, CONTENT_TYPE_PROTORPC);
  }
}
